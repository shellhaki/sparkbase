// Package httpx has the JSON, error and middleware helpers shared by every handler.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/logx"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
)

// Error is the one error shape every endpoint returns.
type Error struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func E(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg, Details: map[string]any{}}
}

func (e *Error) With(k string, v any) *Error {
	e.Details[k] = v
	return e
}

func Unauthorized(msg string) *Error { return E(401, "unauthorized", msg) }
func Forbidden(msg string) *Error    { return E(403, "forbidden", msg) }
func NotFound(what string) *Error    { return E(404, "not_found", what+" not found") }
func Conflict(msg string) *Error     { return E(409, "conflict", msg) }
func Invalid(msg string) *Error      { return E(422, "validation_failed", msg) }
func NotReady(status string) *Error {
	return E(409, "resource_not_ready", "the service is "+status).With("status", status)
}
func Quota(msg string) *Error { return E(429, "quota_exceeded", msg) }

// Handler is an http handler that returns an error instead of writing it.
type Handler func(w http.ResponseWriter, r *http.Request) error

// Wrap turns a Handler into an http.HandlerFunc that renders errors.
func Wrap(log *slog.Logger, h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			WriteError(w, r, log, err)
		}
	}
}

func WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var e *Error
	if !errors.As(err, &e) {
		log.Error("internal error", "err", err.Error(), "path", r.URL.Path, "request_id", logx.RequestID(r.Context()))
		e = E(500, "internal", "something went wrong")
	}
	WriteJSON(w, e.Status, map[string]any{"error": e})
}

func WriteJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return nil
	}
	return json.NewEncoder(w).Encode(v)
}

func OK(w http.ResponseWriter, v any) error { return WriteJSON(w, 200, v) }

func NoContent(w http.ResponseWriter) error {
	w.WriteHeader(204)
	return nil
}

// Decode reads a JSON body (max bytes) into v and rejects unknown fields.
func Decode(r *http.Request, v any, max int64) error {
	if max <= 0 {
		max = 1 << 20
	}
	body := http.MaxBytesReader(nil, r.Body, max)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return Invalid("request body is empty")
		}
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return E(413, "payload_too_large", fmt.Sprintf("request body is larger than %d bytes", max))
		}
		return Invalid("invalid JSON: " + err.Error())
	}
	return nil
}

// ClientIP returns the remote address, honoring X-Forwarded-For from a proxy.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(s int) {
	w.status = s
	w.ResponseWriter.WriteHeader(s)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Observer receives request timing for metrics.
type Observer func(method, route string, status int, d time.Duration)

// Logging adds a request id and logs each request.
func Logging(log *slog.Logger, observe Observer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = sbcrypto.RandomLower(16)
		}
		w.Header().Set("X-Request-Id", id)
		r = r.WithContext(logx.WithRequestID(r.Context(), id))
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(sw, r)
		d := time.Since(start)
		if sw.status == 0 {
			sw.status = 200
		}
		if observe != nil {
			observe(r.Method, r.Pattern, sw.status, d)
		}
		if r.URL.Path == "/health" {
			return
		}
		log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"ms", d.Milliseconds(), "request_id", id)
	})
}

// Recover turns panics into 500 responses.
func Recover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic", "value", fmt.Sprint(v), "path", r.URL.Path)
				WriteError(w, r, log, E(500, "internal", "something went wrong"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORS allows browser apps to call the project API and the dashboard origin
// to call the management API with credentials.
func CORS(dashboardOrigin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			h := w.Header()
			if strings.HasPrefix(r.URL.Path, "/p/") {
				h.Set("Access-Control-Allow-Origin", "*")
			} else if origin == dashboardOrigin {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Add("Vary", "Origin")
			}
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, apikey, X-Request-Id, Idempotency-Key, X-Sparkbase-Client")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Expose-Headers", "X-Request-Id, X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
