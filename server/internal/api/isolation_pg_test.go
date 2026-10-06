package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shellhaki/sparkbase/server/internal/drivers"
	"github.com/shellhaki/sparkbase/server/internal/store"
	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

// pgTenant is one tenant's Postgres service.
type pgTenant struct {
	c   *testenv.Client
	p   *testenv.Project
	id  string
	url string
	db  string
}

func newPGTenant(t *testing.T, e *testenv.Env, admin *testenv.Client, email, plan string) *pgTenant {
	t.Helper()
	c := e.Invite(admin, email, "member")
	p := e.Project(c, "iso")
	body := map[string]any{"kind": "postgres", "name": "db"}
	if plan != "" {
		body["plan"] = plan
	}
	id := c.Do("POST", "/v1/projects/"+p.ID+"/resources", body).Must(t, 202).Str("resource", "id")
	e.WaitStatus(c, id, "active")
	conn := c.Do("GET", "/v1/resources/"+id+"/connection", nil).Must(t, 200)
	return &pgTenant{c: c, p: p, id: id, url: conn.Str("native", "url"), db: conn.Str("native", "database")}
}

func mustConnect(t *testing.T, url string) *pgx.Conn {
	t.Helper()
	conn, err := connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

func expectErr(t *testing.T, conn *pgx.Conn, what, sql string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql, args...); err == nil {
		t.Errorf("%s: expected failure, it succeeded", what)
	}
}

func TestPostgresIsolation(t *testing.T) {
	e := testenv.New(t)
	ctx := context.Background()
	admin := e.Admin()
	// A plan with a short statement timeout so the timeout check is quick.
	admin.Do("POST", "/v1/admin/plans", map[string]any{"id": "postgres-tiny", "kind": "postgres", "name": "Tiny",
		"limits": map[string]any{"storage_mb": 1, "direct_connections": 2, "statement_timeout_ms": 1000, "requests_per_month": 1000}}).Must(t, 201)

	a := newPGTenant(t, e, admin, "a@example.com", "postgres-tiny")
	b := newPGTenant(t, e, admin, "b@example.com", "")
	connA := mustConnect(t, a.url)
	connB := mustConnect(t, b.url)
	if _, err := connB.Exec(ctx, `create table secrets (v text); insert into secrets values ('b-only')`); err != nil {
		t.Fatal(err)
	}

	t.Run("A cannot connect to B's database", func(t *testing.T) {
		if c, err := connect(ctx, swapDB(a.url, b.db)); err == nil {
			c.Close(ctx)
			t.Fatal("A connected to B's database")
		}
	})

	t.Run("A cannot connect to system databases", func(t *testing.T) {
		for _, db := range []string{"postgres", "template1", "template0", "sparkbase"} {
			if c, err := connect(ctx, swapDB(a.url, db)); err == nil {
				c.Close(ctx)
				t.Errorf("A connected to %s", db)
			}
		}
	})

	t.Run("B's credentials only work on B", func(t *testing.T) {
		if c, err := connect(ctx, swapDB(b.url, a.db)); err == nil {
			c.Close(ctx)
			t.Fatal("B connected to A's database")
		}
	})

	t.Run("no role, database or role switching", func(t *testing.T) {
		expectErr(t, connA, "create role", `create role evil login password 'x'`)
		expectErr(t, connA, "create database", `create database evil`)
		expectErr(t, connA, "set role to B's owner", `set role `+ownerRole(b.id))
		expectErr(t, connA, "grant B's owner", `grant `+ownerRole(b.id)+` to current_user`)
		expectErr(t, connA, "alter own limits", `alter role current_user connection limit -1`)
		expectErr(t, connA, "read password hashes", `select * from pg_authid`)
	})

	t.Run("no server files or programs", func(t *testing.T) {
		expectErr(t, connA, "pg_read_file", `select pg_read_file('/etc/passwd')`)
		expectErr(t, connA, "copy from file", `copy (select 1) to '/tmp/sb-evil'`)
		expectErr(t, connA, "copy program", `copy (select 1) to program 'id'`)
		expectErr(t, connA, "lo_import", `select lo_import('/etc/passwd')`)
	})

	t.Run("forbidden extensions", func(t *testing.T) {
		for _, ext := range []string{"dblink", "file_fdw", "postgres_fdw", "plpython3u", "adminpack"} {
			expectErr(t, connA, "extension "+ext, `create extension `+ext)
		}
		expectErr(t, connA, "untrusted language", `create language plperlu`)
	})

	t.Run("no data from other tenants", func(t *testing.T) {
		expectErr(t, connA, "read B's table", `select * from secrets`)
		var n int
		if err := connA.QueryRow(ctx, `select count(*) from pg_stat_activity where datname = $1 and query <> ''`, b.db).Scan(&n); err == nil && n > 0 {
			var q string
			_ = connA.QueryRow(ctx, `select query from pg_stat_activity where datname = $1 and query <> '' limit 1`, b.db).Scan(&q)
			if q != "<insufficient privilege>" {
				t.Errorf("A sees B's queries: %q", q)
			}
		}
	})

	t.Run("statement timeout", func(t *testing.T) {
		start := time.Now()
		expectErr(t, connA, "long query", `select pg_sleep(5)`)
		if time.Since(start) > 4*time.Second {
			t.Fatal("statement timeout not applied")
		}
	})

	t.Run("connection limit", func(t *testing.T) {
		var conns []*pgx.Conn
		defer func() {
			for _, c := range conns {
				c.Close(ctx)
			}
		}()
		refused := false
		for i := 0; i < 12; i++ {
			c, err := connect(ctx, a.url)
			if err != nil {
				if !strings.Contains(err.Error(), "too many connections") {
					t.Fatalf("unexpected error: %v", err)
				}
				refused = true
				break
			}
			conns = append(conns, c)
		}
		if !refused {
			t.Fatal("connection limit not enforced")
		}
	})

	t.Run("project API keys are per project", func(t *testing.T) {
		if r := e.ProjectClient(a.p.Secret).Do("POST", "/p/"+b.p.Ref+"/postgres/db/query", map[string]any{"sql": "select 1"}); r.Status != 401 {
			t.Fatalf("A's key on B's project: %s", r)
		}
		r := e.ProjectClient(a.p.Secret).Do("POST", "/p/"+a.p.Ref+"/postgres/db/query", map[string]any{"sql": "select * from secrets"})
		if r.Status != 400 {
			t.Fatalf("A read B's table over HTTP: %s", r)
		}
		if r := a.c.Do("POST", "/p/"+b.p.Ref+"/postgres/db/query", map[string]any{"sql": "select 1"}); r.Status != 404 {
			t.Fatalf("A's session on B's project: %s", r)
		}
		r = e.ProjectClient(a.p.Secret).Do("POST", "/p/"+a.p.Ref+"/postgres/db/query", map[string]any{"sql": "set role " + ownerRole(b.id)})
		if r.Status != 400 {
			t.Fatalf("SQL over HTTP switched role: %s", r)
		}
	})

	t.Run("write lock at the storage limit", func(t *testing.T) {
		// The tiny plan allows 1 MB; an empty database is larger.
		if err := e.App.SampleUsage(ctx); err != nil {
			t.Fatal(err)
		}
		res := a.c.Do("GET", "/v1/resources/"+a.id, nil).Must(t, 200)
		if res.Get("resource", "writeLocked") != true {
			t.Fatalf("not write-locked: %s", res)
		}
		c := mustConnect(t, a.url)
		expectErr(t, c, "write when locked", `create table x (id int)`)
		if _, err := c.Exec(ctx, `select 1`); err != nil {
			t.Fatalf("reads must still work: %v", err)
		}
		r := e.ProjectClient(a.p.Secret).Do("POST", "/p/"+a.p.Ref+"/postgres/db/query", map[string]any{"sql": "create table y (id int)"})
		if r.Code() != "quota_exceeded" {
			t.Fatalf("API write when locked: %s", r)
		}
		// A bigger plan lifts the lock.
		a.c.Do("PATCH", "/v1/resources/"+a.id, map[string]any{"plan": "postgres-small"}).Must(t, 200)
		res = a.c.Do("GET", "/v1/resources/"+a.id, nil).Must(t, 200)
		if res.Get("resource", "writeLocked") != false {
			t.Fatalf("still locked after plan change: %s", res)
		}
		c2 := mustConnect(t, a.url)
		if _, err := c2.Exec(ctx, `create table z (id int)`); err != nil {
			t.Fatalf("write after unlock: %v", err)
		}
		notes := a.c.Do("GET", "/v1/notifications", nil).Must(t, 200)
		if len(notes.Get("notifications").([]any)) < 2 {
			t.Fatalf("expected breach and clear notifications: %s", notes)
		}
	})

	t.Run("suspended refuses logins", func(t *testing.T) {
		admin.Do("POST", "/v1/admin/resources/"+b.id+"/suspend", nil).Must(t, 200)
		if c, err := connect(ctx, b.url); err == nil {
			c.Close(ctx)
			t.Fatal("suspended database accepted a login")
		}
		admin.Do("POST", "/v1/admin/resources/"+b.id+"/unsuspend", nil).Must(t, 200)
	})

	// The suite must catch a weakened setup: re-open B's database to PUBLIC
	// and the cross-tenant connect check has to start succeeding.
	t.Run("weakened setup is detected", func(t *testing.T) {
		host, err := e.App.Store.PickHost(ctx, "postgres")
		if err != nil {
			t.Fatal(err)
		}
		secret, _ := e.App.Box.OpenString(host.AdminSecretEnc)
		adminConn, err := pgx.Connect(ctx, drivers.AdminURL(host, secret, ""))
		if err != nil {
			t.Fatal(err)
		}
		defer adminConn.Close(ctx)
		if _, err := adminConn.Exec(ctx, `grant connect on database `+b.db+` to public`); err != nil {
			t.Fatal(err)
		}
		c, err := connect(ctx, swapDB(a.url, b.db))
		if _, rerr := adminConn.Exec(ctx, `revoke connect on database `+b.db+` from public`); rerr != nil {
			t.Fatal(rerr)
		}
		if err != nil {
			t.Fatalf("the isolation check did not notice a weakened database: %v", err)
		}
		c.Close(ctx)
	})
}

func ownerRole(id string) string { return drivers.OwnerRole(&store.Resource{ID: id}) }
