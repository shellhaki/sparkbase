// Package logs keeps recent log lines per service (and for the system) in
// Redis, publishes new lines for live tails, and counts per-minute request
// metrics.
package logs

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shellhaki/sparkbase/server/internal/logx"
)

const keep = 500

// Entry is one log line.
type Entry struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`  // info, warn, error
	Source  string         `json:"source"` // api, container, cron, monitor, realtime, backup, system
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type Hub struct {
	R      *redis.Client
	Prefix string
}

func (h *Hub) listKey(stream string) string { return h.Prefix + "logs:" + stream }
func (h *Hub) chanKey(stream string) string { return h.Prefix + "logs:live:" + stream }

// ServiceStream is the stream name for a service.
func ServiceStream(resourceID string) string { return "svc:" + resourceID }

// SystemStream holds the server's own logs.
const SystemStream = "system"

// Push records a line and publishes it to live tails.
func (h *Hub) Push(ctx context.Context, stream string, e Entry) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	pipe := h.R.Pipeline()
	pipe.LPush(ctx, h.listKey(stream), b)
	pipe.LTrim(ctx, h.listKey(stream), 0, keep-1)
	pipe.Expire(ctx, h.listKey(stream), 7*24*time.Hour)
	pipe.Publish(ctx, h.chanKey(stream), b)
	_, _ = pipe.Exec(ctx)
}

// Recent returns up to n lines, oldest first.
func (h *Hub) Recent(ctx context.Context, stream string, n int) ([]Entry, error) {
	raw, err := h.R.LRange(ctx, h.listKey(stream), 0, int64(n-1)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- {
		var e Entry
		if json.Unmarshal([]byte(raw[i]), &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// Follow calls fn for each new line until ctx ends.
func (h *Hub) Follow(ctx context.Context, stream string, fn func(Entry)) error {
	sub := h.R.Subscribe(ctx, h.chanKey(stream))
	defer sub.Close()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-ch:
			if !ok {
				return nil
			}
			var e Entry
			if json.Unmarshal([]byte(m.Payload), &e) == nil {
				fn(e)
			}
		}
	}
}

// Metrics: per-minute counters for each service.

func (h *Hub) minuteKey(resourceID string, t time.Time) string {
	return h.Prefix + "m:" + resourceID + ":" + strconv.FormatInt(t.Unix()/60, 10)
}

// Observe counts one request.
func (h *Hub) Observe(ctx context.Context, resourceID string, status int, d time.Duration) {
	k := h.minuteKey(resourceID, time.Now())
	pipe := h.R.Pipeline()
	pipe.HIncrBy(ctx, k, "requests", 1)
	if status >= 400 {
		pipe.HIncrBy(ctx, k, "errors", 1)
	}
	pipe.HIncrBy(ctx, k, "ms", d.Milliseconds())
	pipe.Expire(ctx, k, 26*time.Hour)
	_, _ = pipe.Exec(ctx)
}

// Point is one minute of request metrics.
type Point struct {
	Minute   time.Time `json:"minute"`
	Requests int64     `json:"requests"`
	Errors   int64     `json:"errors"`
	AvgMs    int64     `json:"avgMs"`
}

// Series returns the last n minutes, oldest first.
func (h *Hub) Series(ctx context.Context, resourceID string, n int) ([]Point, error) {
	now := time.Now()
	pipe := h.R.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, n)
	for i := 0; i < n; i++ {
		cmds[i] = pipe.HGetAll(ctx, h.minuteKey(resourceID, now.Add(-time.Duration(n-1-i)*time.Minute)))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	out := make([]Point, n)
	for i, c := range cmds {
		m := c.Val()
		p := Point{Minute: now.Add(-time.Duration(n-1-i) * time.Minute).Truncate(time.Minute).UTC()}
		p.Requests, _ = strconv.ParseInt(m["requests"], 10, 64)
		p.Errors, _ = strconv.ParseInt(m["errors"], 10, 64)
		ms, _ := strconv.ParseInt(m["ms"], 10, 64)
		if p.Requests > 0 {
			p.AvgMs = ms / p.Requests
		}
		out[i] = p
	}
	return out, nil
}

// TeeHandler copies records at or above a level into the system stream,
// without blocking the caller.
type TeeHandler struct {
	slog.Handler
	ch chan Entry
}

func NewTee(next slog.Handler, hub *Hub) *TeeHandler {
	t := &TeeHandler{Handler: next, ch: make(chan Entry, 1024)}
	go func() {
		for e := range t.ch {
			hub.Push(context.Background(), SystemStream, e)
		}
	}()
	return t
}

func (t *TeeHandler) Handle(ctx context.Context, r slog.Record) error {
	err := t.Handler.Handle(ctx, r)
	fields := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		if logx.IsSensitive(a.Key) {
			fields[a.Key] = "[redacted]"
		} else {
			fields[a.Key] = logx.Scrub(a.Value.String())
		}
		return true
	})
	level := "info"
	if r.Level >= slog.LevelError {
		level = "error"
	} else if r.Level >= slog.LevelWarn {
		level = "warn"
	}
	select {
	case t.ch <- Entry{Time: r.Time.UTC(), Level: level, Source: "system", Message: logx.Scrub(r.Message), Fields: fields}:
	default: // drop rather than block the server
	}
	return err
}

func (t *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TeeHandler{Handler: t.Handler.WithAttrs(attrs), ch: t.ch}
}

func (t *TeeHandler) WithGroup(name string) slog.Handler {
	return &TeeHandler{Handler: t.Handler.WithGroup(name), ch: t.ch}
}
