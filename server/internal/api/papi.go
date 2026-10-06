package api

import (
	"context"
	"errors"

	"github.com/shellhaki/sparkbase/server/internal/logs"
	"net/http"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

// Access levels on the project API.
const (
	accessPublishable = 1
	accessSecret      = 2 // secret key, or the project owner's session or account key
)

// pctx is the caller of a project API route.
type pctx struct {
	Project *store.Project
	Access  int
	// UserToken is an end-user access token from the auth service, if sent.
	UserToken string
	// ResourceID is the service the call reached, set by service().
	ResourceID string
}

type projectHandler func(w http.ResponseWriter, r *http.Request, pc *pctx) error

// papi registers a project API route that needs at least the given access.
// Calls that reach a service are logged to that service's live log and
// counted in its per-minute metrics.
func (s *Server) papi(pattern string, need int, h projectHandler) {
	s.handle(pattern, func(w http.ResponseWriter, r *http.Request) error {
		pc, err := s.projectAuth(r)
		if err != nil {
			return err
		}
		if pc.Access < need {
			return httpx.Forbidden("this route needs the project's secret key; the publishable key only works for auth")
		}
		if err := s.rateLimit(w, r, "project:"+pc.Project.ID, 1200, time.Minute); err != nil {
			return err
		}
		start := time.Now()
		rec := &statusCapture{ResponseWriter: w}
		err = h(rec, r, pc)
		if pc.ResourceID != "" {
			status := rec.status
			if err != nil {
				status = 500
				var e *httpx.Error
				if errors.As(err, &e) {
					status = e.Status
				}
			}
			if status == 0 {
				status = 200
			}
			s.logRequest(pc, r, status, time.Since(start), err)
		}
		return err
	})
}

type statusCapture struct {
	http.ResponseWriter
	status int
}

func (c *statusCapture) WriteHeader(s int) {
	c.status = s
	c.ResponseWriter.WriteHeader(s)
}

func (c *statusCapture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *statusCapture) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (s *Server) logRequest(pc *pctx, r *http.Request, status int, d time.Duration, err error) {
	ctx := context.WithoutCancel(r.Context())
	level := "info"
	if status >= 500 {
		level = "error"
	} else if status >= 400 {
		level = "warn"
	}
	path := strings.TrimPrefix(r.URL.Path, "/p/"+pc.Project.Slug)
	fields := map[string]any{"status": status, "ms": d.Milliseconds(), "method": r.Method}
	if err != nil {
		var e *httpx.Error
		if errors.As(err, &e) {
			fields["code"] = e.Code
			fields["error"] = e.Message
		}
	}
	s.Logs.Push(ctx, logs.ServiceStream(pc.ResourceID), logs.Entry{Level: level, Source: "api", Message: r.Method + " " + path, Fields: fields})
	s.Logs.Observe(ctx, pc.ResourceID, status, d)
}

func (s *Server) projectAuth(r *http.Request) (*pctx, error) {
	ctx := r.Context()
	proj, err := s.Store.GetProjectBySlug(ctx, r.PathValue("ref"))
	if errors.Is(err, store.ErrNotFound) {
		return nil, httpx.NotFound("project")
	}
	if err != nil {
		return nil, err
	}
	if proj.Status != "active" {
		return nil, httpx.E(409, "resource_not_ready", "the project is "+proj.Status)
	}
	pc := &pctx{Project: proj}

	key := strings.TrimSpace(r.Header.Get("apikey"))
	tok := bearer(r)
	if key == "" && (strings.HasPrefix(tok, sbcrypto.PrefixPublishable) || strings.HasPrefix(tok, sbcrypto.PrefixSecret)) {
		key, tok = tok, ""
	}
	if tok != "" && !strings.HasPrefix(tok, sbcrypto.PrefixAccount) {
		pc.UserToken = tok
	}

	switch {
	case strings.HasPrefix(key, sbcrypto.PrefixPublishable) || strings.HasPrefix(key, sbcrypto.PrefixSecret):
		k, err := s.Store.ProjectKeyByHash(ctx, sbcrypto.SHA256Hex(key))
		if errors.Is(err, store.ErrNotFound) || (err == nil && k.ProjectID != proj.ID) {
			return nil, httpx.Unauthorized("invalid project key")
		}
		if err != nil {
			return nil, err
		}
		pc.Access = accessPublishable
		if k.Kind == "secret" {
			pc.Access = accessSecret
		}
		return pc, nil
	case key != "":
		return nil, httpx.Unauthorized("the apikey header must hold a project key (sbp_ or sbs_)")
	}

	// The owner's own session or account key works like the secret key, so
	// the dashboard can browse data.
	if tok != "" && !strings.HasPrefix(tok, sbcrypto.PrefixAccount) {
		return nil, httpx.Unauthorized("send the project key in the apikey header together with a user token")
	}
	p, err := s.authenticate(r)
	if err != nil {
		return nil, httpx.Unauthorized("send a project key in the apikey header")
	}
	if !p.canAccess(proj) {
		return nil, httpx.NotFound("project")
	}
	pc.Access = accessSecret
	return pc, nil
}

// service loads a named service of a kind that is ready for requests, and
// counts the request against its monthly limit.
func (s *Server) service(r *http.Request, pc *pctx, kind, name string) (*store.Resource, error) {
	ctx := r.Context()
	var res *store.Resource
	var err error
	if kind == "auth" {
		res, err = s.Store.ProjectAuthResource(ctx, pc.Project.ID)
	} else {
		res, err = s.Store.GetResourceByName(ctx, pc.Project.ID, kind, name)
	}
	if errors.Is(err, store.ErrNotFound) {
		if kind == "auth" {
			return nil, httpx.NotFound("auth service (add one to the project)")
		}
		return nil, httpx.NotFound(kind + " service " + name)
	}
	if err != nil {
		return nil, err
	}
	pc.ResourceID = res.ID
	if res.Status != "active" {
		return nil, httpx.NotReady(res.Status)
	}
	if limit := res.Limits.Get("requests_per_month"); limit != 0 {
		n, err := s.Counter.Incr(ctx, res.ID)
		if err == nil && limit > 0 && n > limit {
			return nil, httpx.Quota("this service used its monthly project API request limit").With("limit", limit)
		}
	}
	_ = s.Store.TouchResource(ctx, res.ID)
	return res, nil
}

func (s *Server) projectAPIRoutes() {
	s.papi("GET /p/{ref}", accessPublishable, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		out := map[string]any{"ref": pc.Project.Slug, "name": pc.Project.Name}
		if pc.Access >= accessSecret {
			list, err := s.Store.ListResources(r.Context(), pc.Project.ID)
			if err != nil {
				return err
			}
			services := make([]map[string]any, 0, len(list))
			for _, res := range list {
				services = append(services, map[string]any{"kind": res.Kind, "name": res.Name, "status": res.Status, "apiUrl": s.apiURL(&res)})
			}
			out["services"] = services
		}
		return httpx.OK(w, out)
	})
	s.postgresRoutes()
	s.cacheRoutes()
	s.storageRoutes()
	s.vectorRoutes()
	s.queueRoutes()
	s.authServiceRoutes()
}
