// Package s3 is a small S3 client (path-style, AWS Signature Version 4)
// covering what Sparkbase needs from Garage: list, put, get, delete and
// presigned URLs. No SDK dependency.
package s3

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const unsignedPayload = "UNSIGNED-PAYLOAD"

type Client struct {
	Endpoint  string // e.g. http://garage:3900
	Region    string
	AccessKey string
	SecretKey string
	HTTP      *http.Client
	Now       func() time.Time
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// escapePath encodes every path segment per SigV4 (RFC 3986 unreserved kept).
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = uriEncode(s, true)
	}
	return strings.Join(segs, "/")
}

func uriEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' || ch == '~' || (ch == '/' && !encodeSlash) {
			b.WriteByte(ch)
		} else {
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, uriEncode(k, true)+"="+uriEncode(v, true))
		}
	}
	return strings.Join(parts, "&")
}

func hmacSHA256(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (c *Client) signingKey(date string) []byte {
	k := hmacSHA256([]byte("AWS4"+c.SecretKey), date)
	k = hmacSHA256(k, c.Region)
	k = hmacSHA256(k, "s3")
	return hmacSHA256(k, "aws4_request")
}

// signature computes the SigV4 signature for a canonical request.
func (c *Client) signature(method, path, query string, headers map[string]string, payloadHash string, t time.Time) (sig, signedHeaders string) {
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, strings.ToLower(k))
	}
	sort.Strings(names)
	lower := map[string]string{}
	for k, v := range headers {
		lower[strings.ToLower(k)] = strings.TrimSpace(v)
	}
	var ch strings.Builder
	for _, n := range names {
		ch.WriteString(n + ":" + lower[n] + "\n")
	}
	signedHeaders = strings.Join(names, ";")
	canonical := strings.Join([]string{method, path, query, ch.String(), signedHeaders, payloadHash}, "\n")
	date := t.Format("20060102")
	scope := date + "/" + c.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + t.Format("20060102T150405Z") + "\n" + scope + "\n" + sha256Hex([]byte(canonical))
	return hex.EncodeToString(hmacSHA256(c.signingKey(date), toSign)), signedHeaders
}

// Sign adds SigV4 headers to a request. payloadHash may be "" for UNSIGNED-PAYLOAD.
func (c *Client) Sign(req *http.Request, payloadHash string) {
	if payloadHash == "" {
		payloadHash = unsignedPayload
	}
	t := c.now()
	req.Header.Set("X-Amz-Date", t.Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	headers := map[string]string{"host": req.URL.Host}
	for k, v := range req.Header {
		lk := strings.ToLower(k)
		if lk == "x-amz-date" || lk == "x-amz-content-sha256" || lk == "range" || lk == "content-type" || lk == "content-md5" || strings.HasPrefix(lk, "x-amz-") {
			headers[lk] = strings.Join(v, ",")
		}
	}
	sig, signed := c.signature(req.Method, escapePath(req.URL.Path), canonicalQuery(req.URL.Query()), headers, payloadHash, t)
	scope := t.Format("20060102") + "/" + c.Region + "/s3/aws4_request"
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", c.AccessKey, scope, signed, sig))
}

// Presign returns a URL valid for expires that performs method on rawURL.
func (c *Client) Presign(method, rawURL string, expires time.Duration) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	t := c.now()
	q := u.Query()
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", c.AccessKey+"/"+t.Format("20060102")+"/"+c.Region+"/s3/aws4_request")
	q.Set("X-Amz-Date", t.Format("20060102T150405Z"))
	q.Set("X-Amz-Expires", strconv.Itoa(int(expires.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")
	sig, _ := c.signature(method, escapePath(u.Path), canonicalQuery(q), map[string]string{"host": u.Host}, unsignedPayload, t)
	u.RawQuery = canonicalQuery(q) + "&X-Amz-Signature=" + sig
	return u.String(), nil
}

// ObjectURL is the path-style URL of an object.
func ObjectURL(endpoint, bucket, key string) string {
	return strings.TrimRight(endpoint, "/") + "/" + bucket + "/" + escapePath(key)
}

// Error is a non-2xx S3 response.
type Error struct {
	Status  int
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func (e *Error) Error() string { return fmt.Sprintf("s3 %d %s: %s", e.Status, e.Code, e.Message) }

func readError(res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	e := &Error{Status: res.StatusCode}
	_ = xml.Unmarshal(body, e)
	if e.Code == "" {
		e.Message = strings.TrimSpace(string(body))
	}
	return e
}

func (c *Client) do(ctx context.Context, method, rawURL string, body io.Reader, size int64, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.Sign(req, "")
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		defer res.Body.Close()
		return nil, readError(res)
	}
	return res, nil
}

type Object struct {
	Key          string    `xml:"Key" json:"key"`
	Size         int64     `xml:"Size" json:"size"`
	LastModified time.Time `xml:"LastModified" json:"lastModified"`
}

type listResult struct {
	Contents              []Object `xml:"Contents"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken"`
}

// List returns up to limit objects with the prefix, starting after "after".
func (c *Client) List(ctx context.Context, bucket, prefix, after string, limit int) ([]Object, bool, error) {
	q := url.Values{"list-type": {"2"}, "max-keys": {strconv.Itoa(limit)}}
	if prefix != "" {
		q.Set("prefix", prefix)
	}
	if after != "" {
		q.Set("start-after", after)
	}
	res, err := c.do(ctx, "GET", strings.TrimRight(c.Endpoint, "/")+"/"+bucket+"?"+canonicalQuery(q), nil, 0, nil)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()
	var out listResult
	if err := xml.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, false, err
	}
	if out.Contents == nil {
		out.Contents = []Object{}
	}
	return out.Contents, out.IsTruncated, nil
}

func (c *Client) Put(ctx context.Context, bucket, key string, body io.Reader, size int64, contentType string) error {
	h := map[string]string{}
	if contentType != "" {
		h["Content-Type"] = contentType
	}
	res, err := c.do(ctx, "PUT", ObjectURL(c.Endpoint, bucket, key), body, size, h)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// Get returns the object response; the caller closes the body.
func (c *Client) Get(ctx context.Context, bucket, key, rangeHeader string) (*http.Response, error) {
	h := map[string]string{}
	if rangeHeader != "" {
		h["Range"] = rangeHeader
	}
	return c.do(ctx, "GET", ObjectURL(c.Endpoint, bucket, key), nil, 0, h)
}

func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	res, err := c.do(ctx, "DELETE", ObjectURL(c.Endpoint, bucket, key), nil, 0, nil)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}
