package api

import (
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/drivers"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

func objectKeyError(err error) error {
	if errors.Is(err, drivers.ErrBadKey) {
		return httpx.Invalid("object keys are relative paths without empty, '.' or '..' segments")
	}
	return err
}

func signPayload(ref, name, method, key string, exp int64) string {
	return strings.Join([]string{ref, name, method, key, strconv.FormatInt(exp, 10)}, "\n")
}

func (s *Server) signURL(res *store.Resource, secret, method, key string, exp int64) string {
	sig := base64.RawURLEncoding.EncodeToString(sbcrypto.HMAC([]byte(secret), signPayload(res.ProjectSlug, res.Name, method, key, exp)))
	q := url.Values{"m": {method}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {sig}}
	escaped := make([]string, 0)
	for _, seg := range strings.Split(key, "/") {
		escaped = append(escaped, url.PathEscape(seg))
	}
	return s.Cfg.ProjectURL(res.ProjectSlug) + "/storage/" + res.Name + "/signed/" + strings.Join(escaped, "/") + "?" + q.Encode()
}

// checkWrite enforces the plan's upload size and storage quota.
func (s *Server) checkWrite(r *http.Request, res *store.Resource, dir string) (int64, error) {
	if res.WriteLocked {
		return 0, httpx.Quota("the storage service is over its limit; delete objects to upload again")
	}
	maxUpload := res.Limits.Get("max_upload_mb") << 20
	if maxUpload <= 0 {
		maxUpload = 50 << 20
	}
	quota := res.Limits.Get("storage_mb") << 20
	if quota > 0 {
		used, _, err := drivers.DirUsage(dir)
		if err != nil {
			return 0, err
		}
		left := quota - used
		if left <= 0 {
			return 0, httpx.Quota("the storage service is full")
		}
		if left < maxUpload {
			maxUpload = left
		}
	}
	if r.ContentLength > maxUpload {
		return 0, httpx.E(413, "payload_too_large", fmt.Sprintf("uploads are limited to %d bytes here", maxUpload))
	}
	return maxUpload, nil
}

func (s *Server) putObject(w http.ResponseWriter, r *http.Request, res *store.Resource, key string) error {
	dir, _, err := s.Tenants.StorageDir(r.Context(), res)
	if err != nil {
		return err
	}
	max, err := s.checkWrite(r, res, dir)
	if err != nil {
		return err
	}
	n, err := drivers.WriteObject(dir, key, r.Body, max)
	if errors.Is(err, drivers.ErrTooLarge) {
		return httpx.E(413, "payload_too_large", fmt.Sprintf("uploads are limited to %d bytes here", max))
	}
	if err != nil {
		return objectKeyError(err)
	}
	return httpx.WriteJSON(w, 201, map[string]any{"key": key, "size": n})
}

func (s *Server) getObject(w http.ResponseWriter, r *http.Request, res *store.Resource, key string) error {
	dir, _, err := s.Tenants.StorageDir(r.Context(), res)
	if err != nil {
		return err
	}
	p, err := drivers.ObjectPath(dir, key)
	if err != nil {
		return objectKeyError(err)
	}
	f, err := os.Open(p)
	if err != nil {
		return httpx.NotFound("object")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return httpx.NotFound("object")
	}
	ct := mime.TypeByExtension(path.Ext(key))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(ct, "text/html") || strings.Contains(ct, "svg") || strings.Contains(ct, "javascript") {
		// Never let stored files run as pages on the API origin.
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	http.ServeContent(w, r, path.Base(key), info.ModTime(), f)
	return nil
}

func (s *Server) storageRoutes() {
	s.papi("GET /p/{ref}/storage/{name}/objects", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := s.service(r, pc, "storage", r.PathValue("name"))
		if err != nil {
			return err
		}
		if g, err := s.garageFor(r.Context(), res); err != nil || g != nil {
			if err != nil {
				return err
			}
			return g.list(w, r)
		}
		dir, _, err := s.Tenants.StorageDir(r.Context(), res)
		if err != nil {
			return err
		}
		q := r.URL.Query()
		objs, more, err := drivers.ListObjects(dir, q.Get("prefix"), q.Get("after"), queryInt(r, "limit", 1000, 1, 1000))
		if err != nil {
			return err
		}
		out := map[string]any{"objects": objs, "truncated": more}
		if more {
			out["next"] = objs[len(objs)-1].Key
		}
		return httpx.OK(w, out)
	})

	s.papi("PUT /p/{ref}/storage/{name}/objects/{key...}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := s.service(r, pc, "storage", r.PathValue("name"))
		if err != nil {
			return err
		}
		if g, err := s.garageFor(r.Context(), res); err != nil || g != nil {
			if err != nil {
				return err
			}
			return g.put(w, r, r.PathValue("key"))
		}
		return s.putObject(w, r, res, r.PathValue("key"))
	})

	s.papi("GET /p/{ref}/storage/{name}/objects/{key...}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := s.service(r, pc, "storage", r.PathValue("name"))
		if err != nil {
			return err
		}
		if g, err := s.garageFor(r.Context(), res); err != nil || g != nil {
			if err != nil {
				return err
			}
			return g.get(w, r, r.PathValue("key"))
		}
		return s.getObject(w, r, res, r.PathValue("key"))
	})

	s.papi("DELETE /p/{ref}/storage/{name}/objects/{key...}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := s.service(r, pc, "storage", r.PathValue("name"))
		if err != nil {
			return err
		}
		if g, err := s.garageFor(r.Context(), res); err != nil || g != nil {
			if err != nil {
				return err
			}
			return g.del(w, r, r.PathValue("key"))
		}
		dir, _, err := s.Tenants.StorageDir(r.Context(), res)
		if err != nil {
			return err
		}
		ok, err := drivers.DeleteObject(dir, r.PathValue("key"))
		if err != nil {
			return objectKeyError(err)
		}
		return httpx.OK(w, map[string]any{"deleted": ok})
	})

	s.papi("POST /p/{ref}/storage/{name}/sign", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Key       string `json:"key"`
			Method    string `json:"method"`
			ExpiresIn int64  `json:"expiresIn"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if in.Method == "" {
			in.Method = "GET"
		}
		in.Method = strings.ToUpper(in.Method)
		if in.Method != "GET" && in.Method != "PUT" {
			return httpx.Invalid("method must be GET or PUT")
		}
		if in.ExpiresIn == 0 {
			in.ExpiresIn = 3600
		}
		if in.ExpiresIn < 1 || in.ExpiresIn > 7*24*3600 {
			return httpx.Invalid("expiresIn must be 1 to 604800 seconds")
		}
		if _, err := drivers.CleanKey(in.Key); err != nil {
			return objectKeyError(err)
		}
		res, err := s.service(r, pc, "storage", r.PathValue("name"))
		if err != nil {
			return err
		}
		if g, err := s.garageFor(r.Context(), res); err != nil || g != nil {
			if err != nil {
				return err
			}
			u, err := g.sign(in.Method, in.Key, in.ExpiresIn)
			if err != nil {
				return err
			}
			return httpx.OK(w, map[string]any{"url": u, "method": in.Method, "expiresAt": time.Now().Add(time.Duration(in.ExpiresIn) * time.Second).UTC()})
		}
		cred, err := s.Store.CurrentCredential(r.Context(), res.ID)
		if err != nil {
			return err
		}
		c, err := s.OpenCredential(cred)
		if err != nil {
			return err
		}
		exp := time.Now().Add(time.Duration(in.ExpiresIn) * time.Second).Unix()
		return httpx.OK(w, map[string]any{"url": s.signURL(res, c.Secret, in.Method, in.Key, exp), "method": in.Method, "expiresAt": time.Unix(exp, 0).UTC()})
	})

	signed := func(method string) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			ctx := r.Context()
			q := r.URL.Query()
			key := r.PathValue("key")
			exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
			if err != nil || q.Get("m") != method {
				return httpx.Unauthorized("invalid signed URL")
			}
			if time.Now().Unix() > exp {
				return httpx.Unauthorized("this signed URL has expired")
			}
			sig, err := base64.RawURLEncoding.DecodeString(q.Get("sig"))
			if err != nil {
				return httpx.Unauthorized("invalid signed URL")
			}
			proj, err := s.Store.GetProjectBySlug(ctx, r.PathValue("ref"))
			if err != nil {
				return httpx.Unauthorized("invalid signed URL")
			}
			res, err := s.Store.GetResourceByName(ctx, proj.ID, "storage", r.PathValue("name"))
			if err != nil {
				return httpx.Unauthorized("invalid signed URL")
			}
			creds, err := s.Credentials(ctx, res.ID)
			if err != nil {
				return err
			}
			valid := false
			for _, c := range creds {
				want := sbcrypto.HMAC([]byte(c.Secret), signPayload(proj.Slug, res.Name, method, key, exp))
				if hmac.Equal(want, sig) {
					valid = true
					break
				}
			}
			if !valid {
				return httpx.Unauthorized("invalid signed URL")
			}
			if res.Status != "active" || proj.Status != "active" {
				return httpx.NotReady(res.Status)
			}
			if err := s.rateLimit(w, r, "project:"+proj.ID, 1200, time.Minute); err != nil {
				return err
			}
			if method == "PUT" {
				return s.putObject(w, r, res, key)
			}
			return s.getObject(w, r, res, key)
		}
	}
	s.handle("GET /p/{ref}/storage/{name}/signed/{key...}", signed("GET"))
	s.handle("PUT /p/{ref}/storage/{name}/signed/{key...}", signed("PUT"))
}
