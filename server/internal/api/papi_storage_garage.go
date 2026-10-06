package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/drivers"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/s3"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

// garageStore serves the storage project API for services on a Garage host,
// using each service's own S3 key.
type garageStore struct {
	s   *Server
	res *store.Resource
	h   *store.Host
	c   *s3.Client
}

// garageFor returns a garageStore when the service lives on Garage, else nil.
func (s *Server) garageFor(ctx context.Context, res *store.Resource) (*garageStore, error) {
	h, err := s.Tenants.Host(ctx, *res.HostID)
	if err != nil {
		return nil, err
	}
	if h.Config.Backend != "garage" {
		return nil, nil
	}
	c, err := s.Tenants.S3(ctx, res, h)
	if err != nil {
		return nil, err
	}
	return &garageStore{s: s, res: res, h: h, c: c}, nil
}

func s3Error(err error) error {
	var e *s3.Error
	if !errors.As(err, &e) {
		return err
	}
	switch {
	case e.Status == 404 || e.Code == "NoSuchKey":
		return httpx.NotFound("object")
	case strings.Contains(strings.ToLower(e.Code+e.Message), "quota"):
		return httpx.Quota("the storage service is full")
	case e.Status == 403:
		return httpx.NotReady("unavailable")
	}
	return err
}

func (g *garageStore) list(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	limit := queryInt(r, "limit", 1000, 1, 1000)
	objs, more, err := g.c.List(r.Context(), g.res.Ext(), q.Get("prefix"), q.Get("after"), limit)
	if err != nil {
		return s3Error(err)
	}
	out := map[string]any{"objects": objs, "truncated": more}
	if more && len(objs) > 0 {
		out["next"] = objs[len(objs)-1].Key
	}
	return httpx.OK(w, out)
}

func (g *garageStore) put(w http.ResponseWriter, r *http.Request, key string) error {
	if _, err := drivers.CleanKey(key); err != nil {
		return objectKeyError(err)
	}
	if g.res.WriteLocked {
		return httpx.Quota("the storage service is over its limit; delete objects to upload again")
	}
	max := g.res.Limits.Get("max_upload_mb") << 20
	if max <= 0 {
		max = 50 << 20
	}
	if r.ContentLength > max {
		return httpx.E(413, "payload_too_large", fmt.Sprintf("uploads are limited to %d bytes here", max))
	}
	body, size := io.Reader(r.Body), r.ContentLength
	if size < 0 {
		// S3 needs a length: spool unknown-length bodies to disk first.
		tmp, err := os.CreateTemp("", "sb-upload-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()
		n, err := io.Copy(tmp, io.LimitReader(r.Body, max+1))
		if err != nil {
			return err
		}
		if n > max {
			return httpx.E(413, "payload_too_large", fmt.Sprintf("uploads are limited to %d bytes here", max))
		}
		if _, err := tmp.Seek(0, 0); err != nil {
			return err
		}
		body, size = tmp, n
	}
	if err := g.c.Put(r.Context(), g.res.Ext(), key, body, size, r.Header.Get("Content-Type")); err != nil {
		return s3Error(err)
	}
	return httpx.WriteJSON(w, 201, map[string]any{"key": key, "size": size})
}

func (g *garageStore) get(w http.ResponseWriter, r *http.Request, key string) error {
	if _, err := drivers.CleanKey(key); err != nil {
		return objectKeyError(err)
	}
	res, err := g.c.Get(r.Context(), g.res.Ext(), key, r.Header.Get("Range"))
	if err != nil {
		return s3Error(err)
	}
	defer res.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "ETag", "Last-Modified", "Accept-Ranges"} {
		if v := res.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	ct := res.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/html") || strings.Contains(ct, "svg") || strings.Contains(ct, "javascript") {
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	w.WriteHeader(res.StatusCode)
	_, err = io.Copy(w, res.Body)
	return err
}

func (g *garageStore) del(w http.ResponseWriter, r *http.Request, key string) error {
	if _, err := drivers.CleanKey(key); err != nil {
		return objectKeyError(err)
	}
	// S3 DELETE succeeds for missing keys; check first so the answer is honest.
	res, err := g.c.Get(r.Context(), g.res.Ext(), key, "bytes=0-0")
	existed := err == nil
	if res != nil {
		res.Body.Close()
	}
	if err := g.c.Delete(r.Context(), g.res.Ext(), key); err != nil {
		return s3Error(err)
	}
	return httpx.OK(w, map[string]any{"deleted": existed})
}

// sign presigns against the public S3 endpoint so browsers can use it.
func (g *garageStore) sign(method, key string, expiresIn int64) (string, error) {
	endpoint := g.h.Config.S3Public
	if endpoint == "" {
		endpoint = g.h.Config.S3Endpoint
	}
	c := *g.c
	return c.Presign(method, s3.ObjectURL(endpoint, g.res.Ext(), key), time.Duration(expiresIn)*time.Second)
}
