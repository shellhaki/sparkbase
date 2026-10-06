package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

var queueNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

type queueInfo struct {
	Name              string    `json:"name"`
	VisibilityTimeout int       `json:"visibilityTimeout"`
	MaxReceives       int       `json:"maxReceives"`
	CreatedAt         time.Time `json:"createdAt"`
}

func (s *Server) queuePool(r *http.Request, pc *pctx) (*store.Resource, *pgxpool.Pool, error) {
	res, err := s.service(r, pc, "queue", r.PathValue("name"))
	if err != nil {
		return nil, nil, err
	}
	pool, err := s.Tenants.PG(r.Context(), res)
	return res, pool, err
}

func getQueue(ctx context.Context, pool *pgxpool.Pool, name string) (*queueInfo, error) {
	var q queueInfo
	err := pool.QueryRow(ctx, `select name, visibility_timeout, max_receives, created_at from sb_queues where name = $1`, name).
		Scan(&q.Name, &q.VisibilityTimeout, &q.MaxReceives, &q.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("queue " + name)
	}
	if err != nil {
		return nil, queryError(err)
	}
	return &q, nil
}

func (s *Server) queueRoutes() {
	s.papi("GET /p/{ref}/queue/{name}/queues", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		_, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		rows, err := pool.Query(r.Context(), `
			select q.name, q.visibility_timeout, q.max_receives, q.created_at,
			  count(m.id) filter (where not m.dead and m.visible_at <= now()) as ready,
			  count(m.id) filter (where not m.dead and m.visible_at > now()) as delayed_or_in_flight,
			  count(m.id) filter (where m.dead) as dead
			from sb_queues q left join sb_messages m on m.queue = q.name
			group by q.name order by q.name`)
		if err != nil {
			return queryError(err)
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var q queueInfo
			var ready, inflight, dead int64
			if err := rows.Scan(&q.Name, &q.VisibilityTimeout, &q.MaxReceives, &q.CreatedAt, &ready, &inflight, &dead); err != nil {
				return queryError(err)
			}
			out = append(out, map[string]any{"name": q.Name, "visibilityTimeout": q.VisibilityTimeout, "maxReceives": q.MaxReceives,
				"createdAt": q.CreatedAt, "ready": ready, "inFlight": inflight, "dead": dead})
		}
		return httpx.OK(w, map[string]any{"queues": out})
	})

	s.papi("POST /p/{ref}/queue/{name}/queues", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Name              string `json:"name"`
			VisibilityTimeout int    `json:"visibilityTimeout"`
			MaxReceives       int    `json:"maxReceives"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if !queueNameRe.MatchString(in.Name) {
			return httpx.Invalid("queue names are lowercase letters, digits, '-' and '_', starting with a letter")
		}
		if in.VisibilityTimeout == 0 {
			in.VisibilityTimeout = 30
		}
		if in.MaxReceives == 0 {
			in.MaxReceives = 5
		}
		if in.VisibilityTimeout < 1 || in.VisibilityTimeout > 43200 {
			return httpx.Invalid("visibilityTimeout must be 1 to 43200 seconds")
		}
		if in.MaxReceives < 1 || in.MaxReceives > 1000 {
			return httpx.Invalid("maxReceives must be 1 to 1000")
		}
		_, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		var q queueInfo
		err = pool.QueryRow(r.Context(), `
			insert into sb_queues (name, visibility_timeout, max_receives) values ($1, $2, $3)
			on conflict (name) do nothing
			returning name, visibility_timeout, max_receives, created_at`, in.Name, in.VisibilityTimeout, in.MaxReceives).
			Scan(&q.Name, &q.VisibilityTimeout, &q.MaxReceives, &q.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.Conflict("queue " + in.Name + " exists")
		}
		if err != nil {
			return queryError(err)
		}
		return httpx.WriteJSON(w, 201, map[string]any{"queue": q})
	})

	s.papi("DELETE /p/{ref}/queue/{name}/queues/{q}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		_, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		tag, err := pool.Exec(r.Context(), `delete from sb_queues where name = $1`, r.PathValue("q"))
		if err != nil {
			return queryError(err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("queue")
		}
		return httpx.NoContent(w)
	})

	s.papi("POST /p/{ref}/queue/{name}/queues/{q}/send", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Messages []struct {
				Body  json.RawMessage `json:"body"`
				Delay int             `json:"delay"`
			} `json:"messages"`
		}
		if err := httpx.Decode(r, &in, 16<<20); err != nil {
			return err
		}
		if len(in.Messages) == 0 || len(in.Messages) > 500 {
			return httpx.Invalid("send 1 to 500 messages")
		}
		res, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		maxBytes := int(res.Limits.Get("max_message_kb")) << 10
		if maxBytes <= 0 {
			maxBytes = 256 << 10
		}
		ctx := r.Context()
		q, err := getQueue(ctx, pool, r.PathValue("q"))
		if err != nil {
			return err
		}
		batch := &pgx.Batch{}
		for i, m := range in.Messages {
			if len(m.Body) == 0 {
				return httpx.Invalid("every message needs a body").With("message", i)
			}
			if len(m.Body) > maxBytes {
				return httpx.Invalid(fmt.Sprintf("messages are limited to %d bytes on this plan", maxBytes)).With("message", i)
			}
			if m.Delay < 0 || m.Delay > 7*24*3600 {
				return httpx.Invalid("delay must be 0 to 604800 seconds").With("message", i)
			}
			batch.Queue(`insert into sb_messages (queue, body, visible_at) values ($1, $2, now() + make_interval(secs => $3)) returning id`,
				q.Name, string(m.Body), m.Delay)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return queryError(err)
		}
		defer tx.Rollback(context.Background())
		br := tx.SendBatch(ctx, batch)
		ids := make([]int64, 0, len(in.Messages))
		for range in.Messages {
			var id int64
			if err := br.QueryRow().Scan(&id); err != nil {
				br.Close()
				return queryError(err)
			}
			ids = append(ids, id)
		}
		if err := br.Close(); err != nil {
			return queryError(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return queryError(err)
		}
		return httpx.WriteJSON(w, 201, map[string]any{"ids": ids})
	})

	s.papi("POST /p/{ref}/queue/{name}/queues/{q}/receive", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Max               int `json:"max"`
			VisibilityTimeout int `json:"visibilityTimeout"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if in.Max == 0 {
			in.Max = 1
		}
		if in.Max < 1 || in.Max > 100 {
			return httpx.Invalid("max must be 1 to 100")
		}
		_, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		q, err := getQueue(ctx, pool, r.PathValue("q"))
		if err != nil {
			return err
		}
		vt := q.VisibilityTimeout
		if in.VisibilityTimeout > 0 {
			if in.VisibilityTimeout > 43200 {
				return httpx.Invalid("visibilityTimeout must be at most 43200 seconds")
			}
			vt = in.VisibilityTimeout
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return queryError(err)
		}
		defer tx.Rollback(context.Background())
		// Messages received too often move to the dead-letter state.
		if _, err := tx.Exec(ctx, `
			update sb_messages set dead = true
			where queue = $1 and not dead and visible_at <= now() and receives >= $2`, q.Name, q.MaxReceives); err != nil {
			return queryError(err)
		}
		rows, err := tx.Query(ctx, `
			update sb_messages m set visible_at = now() + make_interval(secs => $3), receives = receives + 1
			where id in (
			  select id from sb_messages
			  where queue = $1 and not dead and visible_at <= now()
			  order by id
			  for update skip locked
			  limit $2)
			returning id, body, receives, enqueued_at`, q.Name, in.Max, vt)
		if err != nil {
			return queryError(err)
		}
		msgs := []map[string]any{}
		for rows.Next() {
			var id int64
			var body json.RawMessage
			var receives int
			var enq time.Time
			if err := rows.Scan(&id, &body, &receives, &enq); err != nil {
				rows.Close()
				return queryError(err)
			}
			msgs = append(msgs, map[string]any{"id": id, "body": body, "receives": receives, "enqueuedAt": enq})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return queryError(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"messages": msgs, "visibilityTimeout": vt})
	})

	s.papi("POST /p/{ref}/queue/{name}/queues/{q}/ack", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			IDs []int64 `json:"ids"`
		}
		if err := httpx.Decode(r, &in, 1<<20); err != nil {
			return err
		}
		if len(in.IDs) == 0 || len(in.IDs) > 1000 {
			return httpx.Invalid("send 1 to 1000 ids")
		}
		_, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		tag, err := pool.Exec(r.Context(), `delete from sb_messages where queue = $1 and id = any($2)`, r.PathValue("q"), in.IDs)
		if err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"acked": tag.RowsAffected()})
	})

	s.papi("POST /p/{ref}/queue/{name}/queues/{q}/nack", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			IDs   []int64 `json:"ids"`
			Delay int     `json:"delay"`
		}
		if err := httpx.Decode(r, &in, 1<<20); err != nil {
			return err
		}
		if len(in.IDs) == 0 || len(in.IDs) > 1000 {
			return httpx.Invalid("send 1 to 1000 ids")
		}
		if in.Delay < 0 || in.Delay > 43200 {
			return httpx.Invalid("delay must be 0 to 43200 seconds")
		}
		_, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		tag, err := pool.Exec(r.Context(), `
			update sb_messages set visible_at = now() + make_interval(secs => $3)
			where queue = $1 and id = any($2) and not dead`, r.PathValue("q"), in.IDs, in.Delay)
		if err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"released": tag.RowsAffected()})
	})

	s.papi("GET /p/{ref}/queue/{name}/queues/{q}/stats", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		_, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		q, err := getQueue(ctx, pool, r.PathValue("q"))
		if err != nil {
			return err
		}
		var ready, inflight, dead int64
		var oldest *time.Time
		err = pool.QueryRow(ctx, `
			select count(*) filter (where not dead and visible_at <= now()),
			       count(*) filter (where not dead and visible_at > now()),
			       count(*) filter (where dead),
			       min(enqueued_at) filter (where not dead)
			from sb_messages where queue = $1`, q.Name).Scan(&ready, &inflight, &dead, &oldest)
		if err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"queue": q, "ready": ready, "inFlight": inflight, "dead": dead, "oldestEnqueuedAt": oldest})
	})

	s.papi("GET /p/{ref}/queue/{name}/queues/{q}/dead", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		_, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		rows, err := pool.Query(r.Context(), `
			select id, body, receives, enqueued_at from sb_messages where queue = $1 and dead order by id limit $2`,
			r.PathValue("q"), queryInt(r, "limit", 100, 1, 1000))
		if err != nil {
			return queryError(err)
		}
		defer rows.Close()
		msgs := []map[string]any{}
		for rows.Next() {
			var id int64
			var body json.RawMessage
			var receives int
			var enq time.Time
			if err := rows.Scan(&id, &body, &receives, &enq); err != nil {
				return queryError(err)
			}
			msgs = append(msgs, map[string]any{"id": id, "body": body, "receives": receives, "enqueuedAt": enq})
		}
		return httpx.OK(w, map[string]any{"messages": msgs})
	})

	s.papi("POST /p/{ref}/queue/{name}/queues/{q}/redrive", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		_, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		tag, err := pool.Exec(r.Context(), `update sb_messages set dead = false, receives = 0, visible_at = now() where queue = $1 and dead`, r.PathValue("q"))
		if err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"redriven": tag.RowsAffected()})
	})

	s.papi("POST /p/{ref}/queue/{name}/queues/{q}/purge", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		_, pool, err := s.queuePool(r, pc)
		if err != nil {
			return err
		}
		tag, err := pool.Exec(r.Context(), `delete from sb_messages where queue = $1`, r.PathValue("q"))
		if err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"purged": tag.RowsAffected()})
	})
}
