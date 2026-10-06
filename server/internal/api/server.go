// Package api serves the management API (/v1), the project API (/p/{ref})
// and the internal endpoints.
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/app"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

const sessionCookie = "sb_session"

type Server struct {
	*app.App
	mux *http.ServeMux
}

func New(a *app.App) *Server {
	s := &Server{App: a, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the full middleware chain.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = httpx.CORS(s.Cfg.DashboardURL, h)
	h = httpx.Recover(s.Log, h)
	h = httpx.Logging(s.Log, s.Metrics.ObserveRequest, h)
	return h
}

// principal is the caller of a management API route.
type principal struct {
	User *store.User
	Key  *store.APIKey // nil for session callers
}

func (p *principal) canAccess(proj *store.Project) bool {
	if p.Key != nil && p.Key.ProjectID != nil && *p.Key.ProjectID != proj.ID {
		return false
	}
	return p.User.IsAdmin() || proj.OwnerID == p.User.ID
}

type authed func(w http.ResponseWriter, r *http.Request, p *principal) error

func (s *Server) handle(pattern string, h httpx.Handler) {
	s.mux.HandleFunc(pattern, httpx.Wrap(s.Log, h))
}

// user registers a route that needs a signed-in user or an account key.
func (s *Server) user(pattern string, h authed) {
	s.handle(pattern, func(w http.ResponseWriter, r *http.Request) error {
		p, err := s.authenticate(r)
		if err != nil {
			return err
		}
		if err := s.rateLimit(w, r, "user:"+p.User.ID, 600, time.Minute); err != nil {
			return err
		}
		return h(w, r, p)
	})
}

// admin registers a route for admins only.
func (s *Server) admin(pattern string, h authed) {
	s.user(pattern, func(w http.ResponseWriter, r *http.Request, p *principal) error {
		if !p.User.IsAdmin() {
			return httpx.Forbidden("admins only")
		}
		if p.Key != nil && p.Key.ProjectID != nil {
			return httpx.Forbidden("project-scoped keys cannot use admin routes")
		}
		return h(w, r, p)
	})
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (s *Server) authenticate(r *http.Request) (*principal, error) {
	ctx := r.Context()
	if tok := bearer(r); tok != "" {
		if !strings.HasPrefix(tok, sbcrypto.PrefixAccount) {
			return nil, httpx.Unauthorized("use an account key (sb_...) for the management API")
		}
		key, err := s.Store.APIKeyByHash(ctx, sbcrypto.SHA256Hex(tok))
		if errors.Is(err, store.ErrNotFound) {
			return nil, httpx.Unauthorized("invalid, revoked or expired API key")
		}
		if err != nil {
			return nil, err
		}
		u, err := s.Store.GetUser(ctx, key.UserID)
		if err != nil {
			return nil, err
		}
		if u.DisabledAt != nil {
			return nil, httpx.Unauthorized("this account is disabled")
		}
		return &principal{User: u, Key: key}, nil
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, httpx.Unauthorized("sign in first")
	}
	u, err := s.Store.UserBySession(ctx, sbcrypto.SHA256Hex(c.Value))
	if errors.Is(err, store.ErrNotFound) {
		return nil, httpx.Unauthorized("your session has expired, sign in again")
	}
	if err != nil {
		return nil, err
	}
	if u.DisabledAt != nil {
		return nil, httpx.Unauthorized("this account is disabled")
	}
	return &principal{User: u}, nil
}

// rateLimit applies a sliding window and sets the X-RateLimit headers.
func (s *Server) rateLimit(w http.ResponseWriter, r *http.Request, key string, limit int, window time.Duration) error {
	res, err := s.Limiter.Allow(r.Context(), key, limit, window)
	if err != nil {
		// Fail open on Redis trouble rather than taking the API down.
		s.Log.Warn("rate limiter", "err", err.Error())
		return nil
	}
	h := w.Header()
	h.Set("X-RateLimit-Limit", strconv.Itoa(res.Limit))
	h.Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
	h.Set("X-RateLimit-Reset", strconv.FormatInt(res.Reset.Unix(), 10))
	if !res.Allowed {
		h.Set("Retry-After", strconv.Itoa(int(time.Until(res.Reset).Seconds())+1))
		return httpx.E(429, "rate_limited", "too many requests, slow down")
	}
	return nil
}

func (s *Server) ipLimit(w http.ResponseWriter, r *http.Request, scope string, limit int, window time.Duration) error {
	return s.rateLimit(w, r, scope+":ip:"+httpx.ClientIP(r), limit, window)
}

// startSession creates a session and sets its cookie.
func (s *Server) startSession(ctx context.Context, w http.ResponseWriter, u *store.User) error {
	token := sbcrypto.Token()
	expires := time.Now().Add(30 * 24 * time.Hour)
	if err := s.Store.CreateSession(ctx, u.ID, sbcrypto.SHA256Hex(token), expires); err != nil {
		return err
	}
	_ = s.Store.TouchLogin(ctx, u.ID)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: s.Cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Server) routes() {
	s.handle("GET /health", func(w http.ResponseWriter, r *http.Request) error {
		return httpx.OK(w, map[string]string{"status": "ok"})
	})
	s.accountRoutes()
	s.adminRoutes()
	s.projectRoutes()
	s.projectAPIRoutes()
	s.internalRoutes()
	s.observeRoutes()
	s.consoleRoutes()
	s.handle("/", func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotFound("route " + r.Method + " " + r.URL.Path)
	})
}

func queryInt(r *http.Request, name string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
