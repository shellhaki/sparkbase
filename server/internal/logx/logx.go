// Package logx sets up structured JSON logging with secret redaction.
package logx

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

var sensitiveKeys = []string{"password", "secret", "token", "apikey", "api_key", "authorization", "cookie", "dsn", "connection_string", "database_url", "redis_url"}

// IsSensitive reports whether a log attribute name should never be printed.
func IsSensitive(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

var (
	urlCreds = regexp.MustCompile(`([a-z][a-z0-9+.-]*://[^:/@\s]+:)[^@\s]+@`)
	keyLike  = regexp.MustCompile(`\b(sb|sbp|sbs)_[A-Za-z0-9]{8,}`)
	emailRe  = regexp.MustCompile(`([A-Za-z0-9._%+-])[A-Za-z0-9._%+-]*@([A-Za-z0-9.-]+)`)
)

// Scrub removes credentials from free text: URL passwords, keys and full emails.
func Scrub(s string) string {
	s = urlCreds.ReplaceAllString(s, "${1}[redacted]@")
	s = keyLike.ReplaceAllString(s, "${1}_[redacted]")
	s = emailRe.ReplaceAllString(s, "${1}***@${2}")
	return s
}

func replace(groups []string, a slog.Attr) slog.Attr {
	if IsSensitive(a.Key) {
		return slog.String(a.Key, "[redacted]")
	}
	if a.Key == "email" {
		return slog.String(a.Key, Scrub(a.Value.String()))
	}
	if a.Value.Kind() == slog.KindString {
		return slog.String(a.Key, Scrub(a.Value.String()))
	}
	return a
}

// New returns a JSON logger that redacts sensitive attributes.
func New(w io.Writer, level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lv, ReplaceAttr: replace}))
}

type ctxKey struct{}

// WithRequestID stores a request id in the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// RequestID returns the request id stored in the context, if any.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}

// Discard is a logger that drops everything, for tests.
func Discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
