package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/dockerx"
	"github.com/shellhaki/sparkbase/server/internal/engine"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/logs"
	"github.com/shellhaki/sparkbase/server/internal/logx"
)

// sse prepares a Server-Sent Events response.
func sse(w http.ResponseWriter) (func(event string, v any), error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming is not supported")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	f.Flush()
	return func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		f.Flush()
	}, nil
}

// streamLogs writes the backlog of a stream, then follows it live.
func (s *Server) streamLogs(w http.ResponseWriter, r *http.Request, stream string) error {
	ctx := r.Context()
	recent, err := s.Logs.Recent(ctx, stream, queryInt(r, "tail", 200, 0, 500))
	if err != nil {
		return err
	}
	if r.URL.Query().Get("follow") != "1" {
		return httpx.OK(w, map[string]any{"entries": recent})
	}
	send, err := sse(w)
	if err != nil {
		return err
	}
	for _, e := range recent {
		send("log", e)
	}
	send("ready", map[string]any{})
	// Heartbeats keep proxies from closing an idle stream.
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				fmt.Fprint(w, ": ping\n\n")
				w.(http.Flusher).Flush()
			}
		}
	}()
	return s.Logs.Follow(ctx, stream, func(e logs.Entry) { send("log", e) })
}

func (s *Server) observeRoutes() {
	// Live logs for a service: project API calls and service events
	// (source=api), or the container's own output (source=container).
	s.user("GET /v1/resources/{id}/logs", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		if r.URL.Query().Get("source") != "container" {
			return s.streamLogs(w, r, logs.ServiceStream(res.ID))
		}
		if res.Runtime != "container" || s.Engine == nil {
			return httpx.Invalid("this service does not run in its own container")
		}
		ctx := r.Context()
		name := engine.ContainerName(res)
		tail := queryInt(r, "tail", 200, 0, 2000)
		if r.URL.Query().Get("follow") != "1" {
			var lines []dockerx.LogLine
			err := s.Engine.Docker.Logs(ctx, name, tail, false, func(l dockerx.LogLine) { lines = append(lines, scrubLine(l)) })
			if err != nil {
				return err
			}
			return httpx.OK(w, map[string]any{"lines": lines})
		}
		send, err := sse(w)
		if err != nil {
			return err
		}
		send("ready", map[string]any{})
		// Follow across restarts: when the container stops (sleep), wait
		// and attach again once it runs.
		for ctx.Err() == nil {
			running, _ := s.Engine.Running(ctx, res)
			if !running {
				send("state", map[string]any{"running": false})
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(3 * time.Second):
				}
				continue
			}
			send("state", map[string]any{"running": true})
			_ = s.Engine.Docker.Logs(ctx, name, tail, true, func(l dockerx.LogLine) { send("line", scrubLine(l)) })
			tail = 0
		}
		return nil
	})

	// Per-minute request metrics and resource samples for a service.
	s.user("GET /v1/resources/{id}/metrics", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		ctx := r.Context()
		minutes := queryInt(r, "minutes", 60, 5, 1440)
		reqs, err := s.Logs.Series(ctx, res.ID, minutes)
		if err != nil {
			return err
		}
		samples, err := s.Store.UsageSeries(ctx, res.ID, "", time.Now().Add(-time.Duration(minutes)*time.Minute), time.Now())
		if err != nil {
			return err
		}
		series := map[string][]map[string]any{}
		for _, sm := range samples {
			series[sm.Metric] = append(series[sm.Metric], map[string]any{"t": sm.SampledAt, "v": sm.Value})
		}
		out := map[string]any{"requests": reqs, "samples": series}
		if res.Runtime == "container" && s.Engine != nil {
			running, _ := s.Engine.Running(ctx, res)
			out["awake"] = running
			if running {
				if st, err := s.Engine.Docker.Stats(ctx, engine.ContainerName(res)); err == nil {
					out["live"] = map[string]any{"memoryBytes": st.MemoryBytes, "memoryLimit": st.MemoryLimit, "cpuPercent": st.CPUPercent}
				}
			}
		}
		return httpx.OK(w, out)
	})

	// Live server logs for admins.
	s.admin("GET /v1/admin/logs", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		return s.streamLogs(w, r, logs.SystemStream)
	})
}

// scrubLine removes credentials from a container log line.
func scrubLine(l dockerx.LogLine) dockerx.LogLine {
	l.Text = logx.Scrub(l.Text)
	return l
}

// Redis console: one command at a time, run as the service's own ACL user.
// Keys of common single-key commands get the service prefix automatically.
var prefixedCommands = map[string]bool{
	"get": true, "set": true, "del": true, "exists": true, "expire": true, "ttl": true, "pttl": true, "persist": true,
	"incr": true, "incrby": true, "decr": true, "decrby": true, "append": true, "strlen": true, "type": true, "getdel": true, "getex": true, "setex": true, "setnx": true,
	"hget": true, "hset": true, "hdel": true, "hgetall": true, "hkeys": true, "hvals": true, "hlen": true, "hincrby": true,
	"lpush": true, "rpush": true, "lpop": true, "rpop": true, "lrange": true, "llen": true,
	"sadd": true, "srem": true, "smembers": true, "sismember": true, "scard": true,
	"zadd": true, "zrem": true, "zrange": true, "zscore": true, "zcard": true, "zincrby": true,
	"xadd": true, "xrange": true, "xlen": true,
}

func formatReply(v any, indent string) string {
	switch x := v.(type) {
	case nil:
		return indent + "(nil)"
	case string:
		return indent + fmt.Sprintf("%q", x)
	case int64:
		return indent + fmt.Sprintf("(integer) %d", x)
	case []any:
		if len(x) == 0 {
			return indent + "(empty array)"
		}
		lines := make([]string, len(x))
		for i, e := range x {
			lines[i] = fmt.Sprintf("%s%d) %s", indent, i+1, strings.TrimLeft(formatReply(e, ""), " "))
		}
		return strings.Join(lines, "\n")
	case map[any]any:
		var lines []string
		i := 1
		for k, val := range x {
			lines = append(lines, fmt.Sprintf("%s%d) %v => %s", indent, i, k, strings.TrimLeft(formatReply(val, ""), " ")))
			i++
		}
		return strings.Join(lines, "\n")
	}
	return indent + fmt.Sprint(v)
}

func (s *Server) consoleRoutes() {
	s.papi("POST /p/{ref}/cache/{name}/command", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Args []string `json:"args"`
		}
		if err := httpx.Decode(r, &in, 64<<10); err != nil {
			return err
		}
		if len(in.Args) == 0 || len(in.Args) > 100 {
			return httpx.Invalid("send a command, for example [\"GET\", \"key\"]")
		}
		res, c, err := s.cacheClient(r, pc)
		if err != nil {
			return err
		}
		args := make([]any, len(in.Args))
		for i, a := range in.Args {
			args[i] = a
		}
		cmd := strings.ToLower(in.Args[0])
		prefix := cacheKeyPrefix(res)
		if prefixedCommands[cmd] && len(in.Args) > 1 && !strings.HasPrefix(in.Args[1], prefix) {
			args[1] = prefix + in.Args[1]
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		v, err := c.Do(ctx, args...).Result()
		out := ""
		if err != nil {
			if err.Error() == "redis: nil" {
				out = "(nil)"
			} else {
				out = "(error) " + err.Error()
			}
		} else {
			out = formatReply(v, "")
		}
		return httpx.OK(w, map[string]any{"output": out})
	})

	// Installed extensions, shown as add-ons in the architecture view.
	s.papi("GET /p/{ref}/postgres/{name}/extensions", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := s.service(r, pc, "postgres", r.PathValue("name"))
		if err != nil {
			return err
		}
		pool, err := s.Tenants.PG(r.Context(), res)
		if err != nil {
			return err
		}
		out, err := runStatement(r.Context(), pool, statement{SQL: `
			select e.extname as name, e.extversion as version,
			       a.comment as description
			from pg_extension e left join pg_available_extensions a on a.name = e.extname
			where e.extname <> 'plpgsql' order by 1`}, 100)
		if err != nil {
			return err
		}
		available, err := runStatement(r.Context(), pool, statement{SQL: `
			select name, default_version as version, comment as description from pg_available_extensions
			where name = any(array['pgcrypto','uuid-ossp','pg_trgm','vector','citext','hstore','ltree','btree_gin','btree_gist','pg_stat_statements','postgis','tablefunc','unaccent','fuzzystrmatch','intarray'])
			order by 1`}, 100)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"installed": out.Rows, "available": available.Rows})
	})
}
