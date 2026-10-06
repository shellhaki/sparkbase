package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/logs"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

var (
	channelRe = regexp.MustCompile(`^[a-zA-Z0-9:_.\-/]{1,128}$`)
	tableRe   = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
)

// RealtimeMessage is what subscribers receive.
type RealtimeMessage struct {
	Channel string          `json:"channel"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
	At      time.Time       `json:"at"`
}

type watch struct {
	Postgres string `json:"postgres"`
	Table    string `json:"table"`
}

type realtimeSettings struct {
	Watches []watch `json:"watches"`
}

// listeners holds one LISTEN connection per (realtime service, postgres
// service) while anyone subscribes to its table channels.
type listeners struct {
	mu   sync.Mutex
	live map[string]*listener
}

type listener struct {
	refs   int
	cancel context.CancelFunc
}

var dbListeners = &listeners{live: map[string]*listener{}}

func (s *Server) rtChannel(rid, channel string) string {
	return s.Cfg.RedisPrefix + "rt:" + rid + ":" + channel
}

func (s *Server) publish(ctx context.Context, rt *store.Resource, m RealtimeMessage) error {
	if m.At.IsZero() {
		m.At = time.Now().UTC()
	}
	b, _ := json.Marshal(m)
	if err := s.Redis.Publish(ctx, s.rtChannel(rt.ID, m.Channel), b).Err(); err != nil {
		return err
	}
	_, _ = s.Counter.Incr(ctx, rt.ID+":msg")
	return nil
}

const notifyFunction = `
create or replace function sb_realtime_notify() returns trigger language plpgsql as $fn$
declare
  v text;
begin
  v := json_build_object('schema', TG_TABLE_SCHEMA, 'table', TG_TABLE_NAME, 'op', TG_OP,
    'row', case when TG_OP = 'DELETE' then row_to_json(OLD) else row_to_json(NEW) end)::text;
  if length(v) > 7900 then
    v := json_build_object('schema', TG_TABLE_SCHEMA, 'table', TG_TABLE_NAME, 'op', TG_OP, 'truncated', true)::text;
  end if;
  perform pg_notify('sb_changes', v);
  return null;
end $fn$`

// acquireListener starts (or shares) the LISTEN loop for a postgres service.
func (s *Server) acquireListener(rt, pg *store.Resource) func() {
	key := rt.ID + "|" + pg.ID
	dbListeners.mu.Lock()
	defer dbListeners.mu.Unlock()
	if l, ok := dbListeners.live[key]; ok {
		l.refs++
		return func() { s.releaseListener(key) }
	}
	ctx, cancel := context.WithCancel(context.Background())
	dbListeners.live[key] = &listener{refs: 1, cancel: cancel}
	go s.runListener(ctx, rt, pg)
	return func() { s.releaseListener(key) }
}

func (s *Server) releaseListener(key string) {
	dbListeners.mu.Lock()
	defer dbListeners.mu.Unlock()
	if l, ok := dbListeners.live[key]; ok {
		l.refs--
		if l.refs <= 0 {
			l.cancel()
			delete(dbListeners.live, key)
		}
	}
}

func (s *Server) runListener(ctx context.Context, rt, pg *store.Resource) {
	for ctx.Err() == nil {
		err := func() error {
			pool, err := s.Tenants.PG(ctx, pg)
			if err != nil {
				return err
			}
			conn, err := pool.Acquire(ctx)
			if err != nil {
				return err
			}
			defer conn.Release()
			if _, err := conn.Exec(ctx, "listen sb_changes"); err != nil {
				return err
			}
			defer conn.Exec(context.Background(), "unlisten sb_changes")
			// Keep a sleeping-capable database awake while people watch it.
			go func() {
				t := time.NewTicker(30 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						_ = s.Store.TouchResource(context.Background(), pg.ID)
					}
				}
			}()
			for {
				n, err := conn.Conn().WaitForNotification(ctx)
				if err != nil {
					return err
				}
				var change struct {
					Table string `json:"table"`
					Op    string `json:"op"`
				}
				_ = json.Unmarshal([]byte(n.Payload), &change)
				_ = s.publish(context.Background(), rt, RealtimeMessage{
					Channel: "db:" + pg.Name + ":" + change.Table, Event: strings.ToLower(change.Op), Payload: json.RawMessage(n.Payload),
				})
			}
		}()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.Logs.Push(context.Background(), logs.ServiceStream(rt.ID), logs.Entry{Level: "warn", Source: "realtime", Message: "listening on " + pg.Name + " stopped, retrying", Fields: map[string]any{"error": err.Error()}})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *Server) realtimeRoutes() {
	base := "/p/{ref}/realtime/{name}"
	load := func(r *http.Request, pc *pctx) (*store.Resource, error) {
		return s.service(r, pc, "realtime", r.PathValue("name"))
	}

	s.papi("POST "+base+"/broadcast", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Channel string          `json:"channel"`
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := httpx.Decode(r, &in, 64<<10); err != nil {
			return err
		}
		if !channelRe.MatchString(in.Channel) || strings.HasPrefix(in.Channel, "db:") {
			return httpx.Invalid("channel names use letters, digits and : _ . - / (db: is reserved for table changes)")
		}
		if in.Event == "" {
			in.Event = "message"
		}
		rt, err := load(r, pc)
		if err != nil {
			return err
		}
		if lim := rt.Limits.Get("messages_per_month"); lim > 0 {
			if n, _ := s.Counter.Get(r.Context(), rt.ID+":msg"); n >= lim {
				return httpx.Quota("this realtime service used its monthly message limit")
			}
		}
		if err := s.publish(r.Context(), rt, RealtimeMessage{Channel: in.Channel, Event: in.Event, Payload: in.Payload}); err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"sent": true})
	})

	// Server-Sent Events. Browsers cannot set headers on EventSource, so
	// the key may come as ?apikey=. The publishable key can only join
	// channels that start with "public:".
	s.papi("GET "+base+"/subscribe", accessPublishable, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		channels := r.URL.Query()["channel"]
		if len(channels) == 0 || len(channels) > 50 {
			return httpx.Invalid("pass 1 to 50 channel parameters")
		}
		for _, c := range channels {
			if !channelRe.MatchString(c) {
				return httpx.Invalid("invalid channel " + c)
			}
			if pc.Access < accessSecret && !strings.HasPrefix(c, "public:") {
				return httpx.Forbidden("the publishable key can only subscribe to public: channels")
			}
		}
		rt, err := load(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		connKey := s.Cfg.RedisPrefix + "rtconns:" + rt.ID
		if lim := rt.Limits.Get("max_connections"); lim > 0 {
			if n, _ := s.Redis.Get(ctx, connKey).Int64(); n >= lim {
				return httpx.Quota("this realtime service has its maximum number of subscribers")
			}
		}
		s.Redis.Incr(ctx, connKey)
		s.Redis.Expire(ctx, connKey, 24*time.Hour)
		defer s.Redis.Decr(context.Background(), connKey)

		// Table channels start their database listener.
		var settings realtimeSettings
		_ = json.Unmarshal(rt.Settings, &settings)
		started := map[string]bool{}
		for _, c := range channels {
			parts := strings.SplitN(c, ":", 3)
			if len(parts) == 3 && parts[0] == "db" && !started[parts[1]] {
				pg, err := s.Store.GetResourceByName(ctx, rt.ProjectID, "postgres", parts[1])
				if err == nil && pg.Status == "active" {
					release := s.acquireListener(rt, pg)
					defer release()
					started[parts[1]] = true
				}
			}
		}

		keys := make([]string, len(channels))
		for i, c := range channels {
			keys[i] = s.rtChannel(rt.ID, c)
		}
		sub := s.Redis.Subscribe(ctx, keys...)
		defer sub.Close()
		if _, err := sub.Receive(ctx); err != nil {
			return err
		}
		send, err := sse(w)
		if err != nil {
			return err
		}
		send("ready", map[string]any{"channels": channels})
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ping.C:
				fmt.Fprint(w, ": ping\n\n")
				w.(http.Flusher).Flush()
			case m, ok := <-ch:
				if !ok {
					return nil
				}
				var msg RealtimeMessage
				if json.Unmarshal([]byte(m.Payload), &msg) == nil {
					send("message", msg)
				}
			}
		}
	})

	s.papi("GET "+base+"/watches", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		rt, err := load(r, pc)
		if err != nil {
			return err
		}
		var settings realtimeSettings
		_ = json.Unmarshal(rt.Settings, &settings)
		if settings.Watches == nil {
			settings.Watches = []watch{}
		}
		n, _ := s.Redis.Get(r.Context(), s.Cfg.RedisPrefix+"rtconns:"+rt.ID).Int64()
		msgs, _ := s.Counter.Get(r.Context(), rt.ID+":msg")
		return httpx.OK(w, map[string]any{"watches": settings.Watches, "subscribers": n, "messagesThisMonth": msgs})
	})

	s.papi("POST "+base+"/watches", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in watch
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if !tableRe.MatchString(in.Table) {
			return httpx.Invalid("table must be a lowercase table name in the public schema")
		}
		rt, err := load(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		pg, err := s.Store.GetResourceByName(ctx, rt.ProjectID, "postgres", in.Postgres)
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("postgres service " + in.Postgres)
		}
		if err != nil {
			return err
		}
		if pg.Status != "active" {
			return httpx.NotReady(pg.Status)
		}
		pool, err := s.Tenants.PG(ctx, pg)
		if err != nil {
			return err
		}
		tbl := pgx.Identifier{"public", in.Table}.Sanitize()
		for _, stmt := range []string{notifyFunction,
			`drop trigger if exists sb_realtime on ` + tbl,
			`create trigger sb_realtime after insert or update or delete on ` + tbl + ` for each row execute function sb_realtime_notify()`,
		} {
			if _, err := pool.Exec(ctx, stmt); err != nil {
				return queryError(err)
			}
		}
		var settings realtimeSettings
		_ = json.Unmarshal(rt.Settings, &settings)
		for _, wt := range settings.Watches {
			if wt == in {
				return httpx.OK(w, map[string]any{"watch": in, "channel": "db:" + in.Postgres + ":" + in.Table})
			}
		}
		settings.Watches = append(settings.Watches, in)
		if err := s.Store.SetResourceSettings(ctx, rt.ID, settings); err != nil {
			return err
		}
		return httpx.WriteJSON(w, 201, map[string]any{"watch": in, "channel": "db:" + in.Postgres + ":" + in.Table})
	})

	s.papi("DELETE "+base+"/watches/{postgres}/{table}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		rt, err := load(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		in := watch{Postgres: r.PathValue("postgres"), Table: r.PathValue("table")}
		if !tableRe.MatchString(in.Table) {
			return httpx.NotFound("watch")
		}
		var settings realtimeSettings
		_ = json.Unmarshal(rt.Settings, &settings)
		kept := settings.Watches[:0]
		found := false
		for _, wt := range settings.Watches {
			if wt == in {
				found = true
				continue
			}
			kept = append(kept, wt)
		}
		if !found {
			return httpx.NotFound("watch")
		}
		settings.Watches = kept
		if pg, err := s.Store.GetResourceByName(ctx, rt.ProjectID, "postgres", in.Postgres); err == nil && pg.Status == "active" {
			if pool, err := s.Tenants.PG(ctx, pg); err == nil {
				_, _ = pool.Exec(ctx, `drop trigger if exists sb_realtime on `+pgx.Identifier{"public", in.Table}.Sanitize())
			}
		}
		if err := s.Store.SetResourceSettings(ctx, rt.ID, settings); err != nil {
			return err
		}
		return httpx.NoContent(w)
	})
}
