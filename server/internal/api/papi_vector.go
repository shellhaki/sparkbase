package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

var collectionRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)

type collection struct {
	Name      string    `json:"name"`
	Dimension int       `json:"dimension"`
	Metric    string    `json:"metric"`
	Native    bool      `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	Count     *int64    `json:"count,omitempty"`
}

func vecTable(name string) string { return pgx.Identifier{"vc_" + name}.Sanitize() }

func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

func (s *Server) vectorPool(r *http.Request, pc *pctx) (*store.Resource, *pgxpool.Pool, error) {
	res, err := s.service(r, pc, "vector", r.PathValue("name"))
	if err != nil {
		return nil, nil, err
	}
	pool, err := s.Tenants.PG(r.Context(), res)
	return res, pool, err
}

func getCollection(ctx context.Context, pool *pgxpool.Pool, name string) (*collection, error) {
	if !collectionRe.MatchString(name) {
		return nil, httpx.NotFound("collection")
	}
	var c collection
	err := pool.QueryRow(ctx, `select name, dimension, metric, native, created_at from sb_collections where name = $1`, name).
		Scan(&c.Name, &c.Dimension, &c.Metric, &c.Native, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("collection " + name)
	}
	if err != nil {
		return nil, queryError(err)
	}
	return &c, nil
}

// distanceExpr returns the SQL distance between the embedding column and $1,
// and how to turn the distance into a score where higher is better.
func distanceExpr(c *collection) (dist string, score func(float64) float64) {
	if c.Native {
		switch c.Metric {
		case "l2":
			return "embedding <-> $1::vector", func(d float64) float64 { return -d }
		case "inner_product":
			return "embedding <#> $1::vector", func(d float64) float64 { return -d }
		default:
			return "embedding <=> $1::vector", func(d float64) float64 { return 1 - d }
		}
	}
	switch c.Metric {
	case "l2":
		return "sb_l2(embedding, $1::real[])", func(d float64) float64 { return -d }
	case "inner_product":
		return "sb_neg_ip(embedding, $1::real[])", func(d float64) float64 { return -d }
	default:
		return "sb_cosine(embedding, $1::real[])", func(d float64) float64 { return 1 - d }
	}
}

func (s *Server) vectorRoutes() {
	s.papi("GET /p/{ref}/vector/{name}/collections", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		_, pool, err := s.vectorPool(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		rows, err := pool.Query(ctx, `select name, dimension, metric, native, created_at from sb_collections order by name`)
		if err != nil {
			return queryError(err)
		}
		list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (collection, error) {
			var c collection
			err := row.Scan(&c.Name, &c.Dimension, &c.Metric, &c.Native, &c.CreatedAt)
			return c, err
		})
		if err != nil {
			return queryError(err)
		}
		for i := range list {
			var n int64
			if err := pool.QueryRow(ctx, `select count(*) from `+vecTable(list[i].Name)).Scan(&n); err == nil {
				list[i].Count = &n
			}
		}
		if list == nil {
			list = []collection{}
		}
		return httpx.OK(w, map[string]any{"collections": list})
	})

	s.papi("POST /p/{ref}/vector/{name}/collections", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Name      string `json:"name"`
			Dimension int    `json:"dimension"`
			Metric    string `json:"metric"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if !collectionRe.MatchString(in.Name) {
			return httpx.Invalid("collection names are lowercase letters, digits and underscores, starting with a letter")
		}
		if in.Metric == "" {
			in.Metric = "cosine"
		}
		if in.Metric != "cosine" && in.Metric != "l2" && in.Metric != "inner_product" {
			return httpx.Invalid("metric must be cosine, l2 or inner_product")
		}
		res, pool, err := s.vectorPool(r, pc)
		if err != nil {
			return err
		}
		maxDim := int(res.Limits.Get("max_dimension"))
		if maxDim <= 0 {
			maxDim = 2048
		}
		if in.Dimension < 1 || in.Dimension > maxDim {
			return httpx.Invalid(fmt.Sprintf("dimension must be 1 to %d on this plan", maxDim))
		}
		h, err := s.Tenants.Host(r.Context(), *res.HostID)
		if err != nil {
			return err
		}
		native := h.Config.HasVector
		ctx := r.Context()
		tx, err := pool.Begin(ctx)
		if err != nil {
			return queryError(err)
		}
		defer tx.Rollback(context.Background())
		tag, err := tx.Exec(ctx, `insert into sb_collections (name, dimension, metric, native) values ($1, $2, $3, $4) on conflict do nothing`,
			in.Name, in.Dimension, in.Metric, native)
		if err != nil {
			return queryError(err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.Conflict("collection " + in.Name + " exists")
		}
		col := "real[] not null check (array_length(embedding, 1) = " + strconv.Itoa(in.Dimension) + ")"
		if native {
			col = "vector(" + strconv.Itoa(in.Dimension) + ") not null"
		}
		stmts := []string{`create table ` + vecTable(in.Name) + ` (
			id         text primary key check (length(id) between 1 and 256),
			embedding  ` + col + `,
			metadata   jsonb not null default '{}',
			created_at timestamptz not null default now())`,
			`create index on ` + vecTable(in.Name) + ` using gin (metadata jsonb_path_ops)`,
		}
		if native && in.Dimension <= 2000 {
			ops := map[string]string{"cosine": "vector_cosine_ops", "l2": "vector_l2_ops", "inner_product": "vector_ip_ops"}[in.Metric]
			stmts = append(stmts, `create index on `+vecTable(in.Name)+` using hnsw (embedding `+ops+`)`)
		}
		for _, st := range stmts {
			if _, err := tx.Exec(ctx, st); err != nil {
				return queryError(err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return queryError(err)
		}
		return httpx.WriteJSON(w, 201, map[string]any{"collection": collection{Name: in.Name, Dimension: in.Dimension, Metric: in.Metric, CreatedAt: time.Now().UTC()}})
	})

	s.papi("DELETE /p/{ref}/vector/{name}/collections/{c}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		_, pool, err := s.vectorPool(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		c, err := getCollection(ctx, pool, r.PathValue("c"))
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return queryError(err)
		}
		defer tx.Rollback(context.Background())
		if _, err := tx.Exec(ctx, `drop table if exists `+vecTable(c.Name)); err != nil {
			return queryError(err)
		}
		if _, err := tx.Exec(ctx, `delete from sb_collections where name = $1`, c.Name); err != nil {
			return queryError(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return queryError(err)
		}
		return httpx.NoContent(w)
	})

	s.papi("POST /p/{ref}/vector/{name}/collections/{c}/upsert", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Items []struct {
				ID       string          `json:"id"`
				Vector   []float32       `json:"vector"`
				Metadata json.RawMessage `json:"metadata"`
			} `json:"items"`
		}
		if err := httpx.Decode(r, &in, 32<<20); err != nil {
			return err
		}
		if len(in.Items) == 0 || len(in.Items) > 1000 {
			return httpx.Invalid("send 1 to 1000 items")
		}
		_, pool, err := s.vectorPool(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		c, err := getCollection(ctx, pool, r.PathValue("c"))
		if err != nil {
			return err
		}
		batch := &pgx.Batch{}
		cast := "$2::real[]"
		if c.Native {
			cast = "$2::vector"
		}
		sql := `insert into ` + vecTable(c.Name) + ` (id, embedding, metadata) values ($1, ` + cast + `, coalesce($3::jsonb, '{}'))
			on conflict (id) do update set embedding = excluded.embedding, metadata = excluded.metadata`
		for i, it := range in.Items {
			if it.ID == "" || len(it.ID) > 256 {
				return httpx.Invalid("every item needs an id of 1 to 256 characters").With("item", i)
			}
			if len(it.Vector) != c.Dimension {
				return httpx.Invalid(fmt.Sprintf("vector has %d values; the collection has dimension %d", len(it.Vector), c.Dimension)).With("item", i)
			}
			var meta *string
			if len(it.Metadata) > 0 && string(it.Metadata) != "null" {
				if it.Metadata[0] != '{' {
					return httpx.Invalid("metadata must be an object").With("item", i)
				}
				m := string(it.Metadata)
				meta = &m
			}
			var vec any = it.Vector
			if c.Native {
				vec = vectorLiteral(it.Vector)
			}
			batch.Queue(sql, it.ID, vec, meta)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return queryError(err)
		}
		defer tx.Rollback(context.Background())
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return queryError(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"upserted": len(in.Items)})
	})

	s.papi("POST /p/{ref}/vector/{name}/collections/{c}/query", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Vector         []float32       `json:"vector"`
			TopK           int             `json:"topK"`
			Filter         json.RawMessage `json:"filter"`
			IncludeVectors bool            `json:"includeVectors"`
		}
		if err := httpx.Decode(r, &in, 1<<20); err != nil {
			return err
		}
		if in.TopK == 0 {
			in.TopK = 10
		}
		if in.TopK < 1 || in.TopK > 1000 {
			return httpx.Invalid("topK must be 1 to 1000")
		}
		_, pool, err := s.vectorPool(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		c, err := getCollection(ctx, pool, r.PathValue("c"))
		if err != nil {
			return err
		}
		if len(in.Vector) != c.Dimension {
			return httpx.Invalid(fmt.Sprintf("vector has %d values; the collection has dimension %d", len(in.Vector), c.Dimension))
		}
		dist, score := distanceExpr(c)
		var filter *string
		if len(in.Filter) > 0 && string(in.Filter) != "null" {
			if in.Filter[0] != '{' {
				return httpx.Invalid("filter must be an object of metadata values to match")
			}
			f := string(in.Filter)
			filter = &f
		}
		vecCol := "null::text"
		if in.IncludeVectors {
			vecCol = "embedding::text"
		}
		var q any = in.Vector
		if c.Native {
			q = vectorLiteral(in.Vector)
		}
		rows, err := pool.Query(ctx, `
			select id, metadata, `+dist+` as distance, `+vecCol+`
			from `+vecTable(c.Name)+`
			where ($2::jsonb is null or metadata @> $2::jsonb)
			order by distance limit $3`, q, filter, in.TopK)
		if err != nil {
			return queryError(err)
		}
		defer rows.Close()
		matches := []map[string]any{}
		for rows.Next() {
			var id string
			var meta map[string]any
			var d *float64
			var vec *string
			if err := rows.Scan(&id, &meta, &d, &vec); err != nil {
				return queryError(err)
			}
			m := map[string]any{"id": id, "metadata": meta}
			if d != nil {
				m["distance"], m["score"] = *d, score(*d)
			}
			if vec != nil {
				m["vector"] = parseVectorText(*vec)
			}
			matches = append(matches, m)
		}
		if err := rows.Err(); err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"matches": matches})
	})

	s.papi("GET /p/{ref}/vector/{name}/collections/{c}/items/{id}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		_, pool, err := s.vectorPool(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		c, err := getCollection(ctx, pool, r.PathValue("c"))
		if err != nil {
			return err
		}
		var meta map[string]any
		var vec string
		var created time.Time
		err = pool.QueryRow(ctx, `select metadata, embedding::text, created_at from `+vecTable(c.Name)+` where id = $1`, r.PathValue("id")).Scan(&meta, &vec, &created)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("item")
		}
		if err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"id": r.PathValue("id"), "metadata": meta, "vector": parseVectorText(vec), "createdAt": created})
	})

	s.papi("POST /p/{ref}/vector/{name}/collections/{c}/delete", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := httpx.Decode(r, &in, 4<<20); err != nil {
			return err
		}
		if len(in.IDs) == 0 || len(in.IDs) > 10000 {
			return httpx.Invalid("send 1 to 10000 ids")
		}
		_, pool, err := s.vectorPool(r, pc)
		if err != nil {
			return err
		}
		c, err := getCollection(r.Context(), pool, r.PathValue("c"))
		if err != nil {
			return err
		}
		tag, err := pool.Exec(r.Context(), `delete from `+vecTable(c.Name)+` where id = any($1)`, in.IDs)
		if err != nil {
			return queryError(err)
		}
		return httpx.OK(w, map[string]any{"deleted": tag.RowsAffected()})
	})
}

// parseVectorText reads "[1,2,3]" (pgvector) or "{1,2,3}" (real[]).
func parseVectorText(s string) []float64 {
	s = strings.Trim(s, "[]{}")
	if s == "" {
		return []float64{}
	}
	parts := strings.Split(s, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		f, _ := strconv.ParseFloat(strings.TrimSpace(p), 64)
		out = append(out, f)
	}
	return out
}
