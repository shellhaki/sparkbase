package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
)

const (
	defaultRowLimit = 1000
	maxRowLimit     = 10000
	maxResultBytes  = 8 << 20
)

type statement struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params"`
}

type queryResult struct {
	Command  string           `json:"command"`
	RowCount int64            `json:"rowCount"`
	Fields   []field          `json:"fields"`
	Rows     []map[string]any `json:"rows"`
	Truncate bool             `json:"truncated,omitempty"`
}

type field struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// queryError maps a Postgres error to an API error.
func queryError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		e := httpx.E(400, "query_failed", pgErr.Message).With("pgCode", pgErr.Code)
		if pgErr.Detail != "" {
			e.With("detail", pgErr.Detail)
		}
		if pgErr.Hint != "" {
			e.With("hint", pgErr.Hint)
		}
		if pgErr.Code == "25006" {
			e = httpx.Quota("the database is over its storage limit and is read-only until data is deleted").With("pgCode", pgErr.Code)
		}
		if pgErr.Code == "57014" {
			e = httpx.E(400, "query_failed", "the statement timed out").With("pgCode", pgErr.Code)
		}
		return e
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return httpx.E(504, "timeout", "the database did not answer in time")
	}
	return err
}

// normalizeParams turns JSON values into types pgx encodes sensibly:
// objects and arrays of objects become JSON text.
func normalizeParams(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		switch x := v.(type) {
		case map[string]any:
			b, _ := json.Marshal(x)
			out[i] = string(b)
		case []any:
			simple := true
			for _, e := range x {
				switch e.(type) {
				case map[string]any, []any:
					simple = false
				}
			}
			if simple {
				out[i] = x
			} else {
				b, _ := json.Marshal(x)
				out[i] = string(b)
			}
		case float64:
			if x == float64(int64(x)) && x < 1e15 && x > -1e15 {
				out[i] = int64(x)
			} else {
				out[i] = x
			}
		default:
			out[i] = v
		}
	}
	return out
}

// jsonValue converts a scanned Postgres value into something JSON can hold.
func jsonValue(v any) any {
	switch x := v.(type) {
	case nil, bool, string, int16, int32, int64, float32, float64:
		return x
	case [16]byte:
		return fmt.Sprintf("%x-%x-%x-%x-%x", x[0:4], x[4:6], x[6:8], x[8:10], x[10:16])
	case []byte:
		return "\\x" + hex.EncodeToString(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case pgtype.Numeric:
		if !x.Valid {
			return nil
		}
		if f, err := x.Float64Value(); err == nil && x.Exp >= -6 && new(big.Int).Abs(x.Int).BitLen() < 50 {
			return f.Float64
		}
		b, _ := x.MarshalJSON()
		return json.RawMessage(b)
	case netip.Prefix:
		return x.String()
	case netip.Addr:
		return x.String()
	case time.Duration:
		return x.String()
	case pgtype.Interval, pgtype.Time:
		b, _ := json.Marshal(x)
		return json.RawMessage(b)
	case map[string]any:
		return x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsonValue(e)
		}
		return out
	case fmt.Stringer:
		return x.String()
	}
	return fmt.Sprint(v)
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func runStatement(ctx context.Context, q querier, st statement, rowLimit int) (*queryResult, error) {
	if st.SQL == "" {
		return nil, httpx.Invalid("sql is required")
	}
	rows, err := q.Query(ctx, st.SQL, normalizeParams(st.Params)...)
	if err != nil {
		return nil, queryError(err)
	}
	defer rows.Close()
	descs := rows.FieldDescriptions()
	res := &queryResult{Fields: make([]field, len(descs)), Rows: []map[string]any{}}
	tm := pgtype.NewMap()
	for i, d := range descs {
		name := "unknown"
		if t, ok := tm.TypeForOID(d.DataTypeOID); ok {
			name = t.Name
		}
		res.Fields[i] = field{Name: d.Name, Type: name}
	}
	size := 0
	for rows.Next() {
		if len(res.Rows) >= rowLimit || size > maxResultBytes {
			res.Truncate = true
			continue
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, queryError(err)
		}
		row := make(map[string]any, len(vals))
		for i, v := range vals {
			jv := jsonValue(v)
			row[descs[i].Name] = jv
			if s, ok := jv.(string); ok {
				size += len(s)
			} else {
				size += 16
			}
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, queryError(err)
	}
	tag := rows.CommandTag()
	res.Command = tag.String()
	if f := []rune(res.Command); len(f) > 0 {
		for i, c := range f {
			if c == ' ' {
				res.Command = string(f[:i])
				break
			}
		}
	}
	res.RowCount = tag.RowsAffected()
	return res, nil
}

func (s *Server) postgresRoutes() {
	s.papi("POST /p/{ref}/postgres/{name}/query", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			statement
			RowLimit int `json:"rowLimit"`
		}
		if err := httpx.Decode(r, &in, 1<<20); err != nil {
			return err
		}
		return s.runQueries(w, r, pc, []statement{in.statement}, in.RowLimit, false)
	})

	s.papi("POST /p/{ref}/postgres/{name}/transaction", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Statements []statement `json:"statements"`
			RowLimit   int         `json:"rowLimit"`
		}
		if err := httpx.Decode(r, &in, 4<<20); err != nil {
			return err
		}
		if len(in.Statements) == 0 || len(in.Statements) > 100 {
			return httpx.Invalid("send 1 to 100 statements")
		}
		return s.runQueries(w, r, pc, in.Statements, in.RowLimit, true)
	})

	s.papi("GET /p/{ref}/postgres/{name}/schema", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := s.service(r, pc, "postgres", r.PathValue("name"))
		if err != nil {
			return err
		}
		pool, err := s.Tenants.PG(r.Context(), res)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		cols, err := runStatement(ctx, pool, statement{SQL: `
			select c.table_schema as schema, c.table_name as table, c.column_name as column,
			       c.data_type as type, c.is_nullable = 'YES' as nullable, c.column_default as default
			from information_schema.columns c
			join information_schema.tables t on t.table_schema = c.table_schema and t.table_name = c.table_name
			where c.table_schema not in ('pg_catalog','information_schema') and c.table_schema not like 'pg\_%'
			  and t.table_type = 'BASE TABLE'
			order by c.table_schema, c.table_name, c.ordinal_position`}, maxRowLimit)
		if err != nil {
			return err
		}
		idx, err := runStatement(ctx, pool, statement{SQL: `
			select schemaname as schema, tablename as table, indexname as name, indexdef as definition
			from pg_indexes where schemaname not in ('pg_catalog','information_schema')
			order by schemaname, tablename, indexname`}, maxRowLimit)
		if err != nil {
			return err
		}
		est, err := runStatement(ctx, pool, statement{SQL: `
			select n.nspname as schema, c.relname as table, greatest(c.reltuples, 0)::bigint as rows,
			       pg_total_relation_size(c.oid) as bytes
			from pg_class c join pg_namespace n on n.oid = c.relnamespace
			where c.relkind in ('r','p') and n.nspname not in ('pg_catalog','information_schema') and n.nspname not like 'pg\_%'
			order by 1, 2`}, maxRowLimit)
		if err != nil {
			return err
		}
		type table struct {
			Schema  string           `json:"schema"`
			Name    string           `json:"name"`
			Rows    any              `json:"estimatedRows"`
			Bytes   any              `json:"bytes"`
			Columns []map[string]any `json:"columns"`
			Indexes []map[string]any `json:"indexes"`
		}
		tables := []*table{}
		byName := map[string]*table{}
		get := func(schema, name string) *table {
			k := schema + "." + name
			if t, ok := byName[k]; ok {
				return t
			}
			t := &table{Schema: schema, Name: name, Columns: []map[string]any{}, Indexes: []map[string]any{}}
			byName[k] = t
			tables = append(tables, t)
			return t
		}
		for _, row := range est.Rows {
			t := get(row["schema"].(string), row["table"].(string))
			t.Rows, t.Bytes = row["rows"], row["bytes"]
		}
		for _, row := range cols.Rows {
			t := get(row["schema"].(string), row["table"].(string))
			t.Columns = append(t.Columns, map[string]any{"name": row["column"], "type": row["type"], "nullable": row["nullable"], "default": row["default"]})
		}
		for _, row := range idx.Rows {
			if t, ok := byName[row["schema"].(string)+"."+row["table"].(string)]; ok {
				t.Indexes = append(t.Indexes, map[string]any{"name": row["name"], "definition": row["definition"]})
			}
		}
		return httpx.OK(w, map[string]any{"tables": tables})
	})
}

func (s *Server) runQueries(w http.ResponseWriter, r *http.Request, pc *pctx, stmts []statement, rowLimit int, atomic bool) error {
	res, err := s.service(r, pc, "postgres", r.PathValue("name"))
	if err != nil {
		return err
	}
	if rowLimit <= 0 {
		rowLimit = defaultRowLimit
	}
	if rowLimit > maxRowLimit {
		rowLimit = maxRowLimit
	}
	pool, err := s.Tenants.PG(r.Context(), res)
	if err != nil {
		return err
	}
	timeout := time.Duration(res.Limits.Get("statement_timeout_ms"))*time.Millisecond + 5*time.Second
	if timeout < 10*time.Second {
		timeout = 35 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	if !atomic {
		out, err := runStatement(ctx, pool, stmts[0], rowLimit)
		if err != nil {
			return err
		}
		return httpx.OK(w, out)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return queryError(err)
	}
	defer tx.Rollback(context.Background())
	results := make([]*queryResult, 0, len(stmts))
	for i, st := range stmts {
		out, err := runStatement(ctx, tx, st, rowLimit)
		if err != nil {
			var e *httpx.Error
			if errors.As(err, &e) {
				e.With("statement", i)
			}
			return err
		}
		results = append(results, out)
	}
	if err := tx.Commit(ctx); err != nil {
		return queryError(err)
	}
	return httpx.OK(w, map[string]any{"results": results})
}
