package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

// nativeURL fetches a service's native connection string.
func nativeURL(t *testing.T, c *testenv.Client, id string) string {
	t.Helper()
	return c.Do("GET", "/v1/resources/"+id+"/connection", nil).Must(t, 200).Str("native", "url")
}

func connect(ctx context.Context, url string) (*pgx.Conn, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return pgx.Connect(cctx, url)
}

func swapDB(url, db string) string {
	i := strings.LastIndex(url, "/")
	j := strings.Index(url[i:], "?")
	return url[:i+1] + db + url[i+j:]
}

func TestPostgresLifecycleAndProjectAPI(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	p := e.Project(admin, "app")
	res := admin.Do("POST", "/v1/projects/"+p.ID+"/resources", map[string]any{"kind": "postgres", "name": "main"}).Must(t, 202)
	if res.Str("resource", "status") != "provisioning" {
		t.Fatalf("create: %s", res)
	}
	id := res.Str("resource", "id")
	sec := e.ProjectClient(p.Secret)

	// Not ready yet (the worker may already have finished, so allow both).
	e.WaitStatus(admin, id, "active")

	// Native connection works and can create tables.
	ctx := context.Background()
	conn, err := connect(ctx, nativeURL(t, admin, id))
	if err != nil {
		t.Fatalf("native connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `create table todos (id serial primary key, title text not null, done bool default false)`); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)

	// Project API: query with params.
	sec.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "insert into todos (title) values ($1), ($2)", "params": []any{"a", "b"}}).Must(t, 200)
	q := sec.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select id, title, done from todos order by id"}).Must(t, 200)
	if len(q.Get("rows").([]any)) != 2 || q.Str("rows", "1", "title") != "b" {
		t.Fatalf("rows: %s", q)
	}
	// Errors carry the Postgres code.
	bad := sec.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select * from nope"})
	if bad.Status != 400 || bad.Code() != "query_failed" {
		t.Fatalf("bad sql: %s", bad)
	}
	// Only one statement per query call.
	if r := sec.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select 1; select 2"}); r.Status != 400 {
		t.Fatalf("multi statement: %s", r)
	}
	// Transactions are atomic.
	tr := sec.Do("POST", "/p/"+p.Ref+"/postgres/main/transaction", map[string]any{"statements": []any{
		map[string]any{"sql": "insert into todos (title) values ('c')"},
		map[string]any{"sql": "insert into todos (title) values (null)"},
	}})
	if tr.Status != 400 {
		t.Fatalf("transaction should fail: %s", tr)
	}
	n := sec.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select count(*)::int as n from todos"}).Must(t, 200)
	if n.Get("rows", "0", "n").(float64) != 2 {
		t.Fatalf("transaction not rolled back: %s", n)
	}
	schema := sec.Do("GET", "/p/"+p.Ref+"/postgres/main/schema", nil).Must(t, 200)
	if schema.Str("tables", "0", "name") != "todos" {
		t.Fatalf("schema: %s", schema)
	}

	// The publishable key cannot run SQL.
	if r := e.ProjectClient(p.Publishable).Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select 1"}); r.Status != 403 {
		t.Fatalf("publishable key ran SQL: %s", r)
	}
	// No key at all.
	if r := e.Client().Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select 1"}); r.Status != 401 {
		t.Fatalf("no key: %s", r)
	}
	// The owner's session works like the secret key (dashboard).
	admin.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select 1"}).Must(t, 200)

	// Pause blocks native logins and the project API; resume restores both.
	admin.Do("POST", "/v1/resources/"+id+"/pause", nil).Must(t, 200)
	if _, err := connect(ctx, nativeURL(t, admin, id)); err == nil {
		t.Fatal("paused database accepted a login")
	}
	if r := sec.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select 1"}); r.Code() != "resource_not_ready" {
		t.Fatalf("paused API: %s", r)
	}
	admin.Do("POST", "/v1/resources/"+id+"/resume", nil).Must(t, 200)
	sec.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select 1"}).Must(t, 200)

	// Rotation: old and new credentials both work during the grace period,
	// the old one stops after it.
	oldURL := nativeURL(t, admin, id)
	admin.Do("PATCH", "/v1/admin/settings", map[string]any{"rotationGraceSeconds": 0}).Must(t, 200)
	admin.Do("POST", "/v1/resources/"+id+"/credentials/rotate", nil).Must(t, 204)
	newURL := nativeURL(t, admin, id)
	if newURL == oldURL {
		t.Fatal("rotation did not change credentials")
	}
	if err := e.App.RevokeDue(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := connect(ctx, oldURL); err == nil {
		t.Fatal("rotated-out credential still works")
	}
	c2, err := connect(ctx, newURL)
	if err != nil {
		t.Fatalf("new credential: %v", err)
	}
	var cnt int
	if err := c2.QueryRow(ctx, "select count(*) from todos").Scan(&cnt); err != nil || cnt != 2 {
		t.Fatalf("data after rotation: %d %v", cnt, err)
	}
	c2.Close(ctx)
	sec.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select count(*) from todos"}).Must(t, 200)

	// Delete removes the database.
	admin.Do("DELETE", "/v1/resources/"+id, nil).Must(t, 202)
	deadline := time.Now().Add(20 * time.Second)
	for admin.Do("GET", "/v1/resources/"+id, nil).Status != 404 {
		if time.Now().After(deadline) {
			t.Fatal("service never deleted")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := connect(ctx, newURL); err == nil {
		t.Fatal("deleted database still reachable")
	}
}

func TestServiceRulesAndOwnership(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	member := e.Invite(admin, "m@example.com", "member")
	p := e.Project(member, "mine")
	other := e.Project(admin, "admins")

	// Members cannot see other users' projects.
	if r := member.Do("GET", "/v1/projects/"+other.ID, nil); r.Status != 404 {
		t.Fatalf("member saw admin project: %s", r)
	}
	if r := member.Do("POST", "/v1/projects/"+other.ID+"/resources", map[string]any{"kind": "postgres", "name": "x"}); r.Status != 404 {
		t.Fatalf("member added to admin project: %s", r)
	}
	// Names and kinds are validated.
	if r := member.Do("POST", "/v1/projects/"+p.ID+"/resources", map[string]any{"kind": "mongo", "name": "x"}); r.Status != 422 {
		t.Fatalf("bad kind: %s", r)
	}
	if r := member.Do("POST", "/v1/projects/"+p.ID+"/resources", map[string]any{"kind": "postgres", "name": "Bad Name"}); r.Status != 422 {
		t.Fatalf("bad name: %s", r)
	}
	// The per-user cap.
	admin.Do("PATCH", "/v1/admin/settings", map[string]any{"resourceCap": 1}).Must(t, 200)
	id := e.Service(member, p, "postgres", "db")
	r := member.Do("POST", "/v1/projects/"+p.ID+"/resources", map[string]any{"kind": "postgres", "name": "db2"})
	if r.Status != 403 || r.Code() != "resource_limit_reached" {
		t.Fatalf("cap: %s", r)
	}
	// Duplicate names in a project conflict.
	admin.Do("PATCH", "/v1/admin/settings", map[string]any{"resourceCap": 10}).Must(t, 200)
	if r := member.Do("POST", "/v1/projects/"+p.ID+"/resources", map[string]any{"kind": "cache", "name": "db"}); r.Status != 409 {
		t.Fatalf("duplicate name: %s", r)
	}
	// Plan change down is blocked when usage is over the target plan.
	if err := e.App.Store.InsertUsage(context.Background(), id, time.Now(), map[string]int64{"storage_bytes": 20 << 30}); err != nil {
		t.Fatal(err)
	}
	r = member.Do("PATCH", "/v1/resources/"+id, map[string]any{"plan": "postgres-medium"})
	if r.Status != 409 || r.Code() != "plan_change_blocked" {
		t.Fatalf("plan change: %s", r)
	}
	member.Do("PATCH", "/v1/resources/"+id, map[string]any{"plan": "postgres-large"}).Must(t, 200)
	// Suspend: the owner cannot resume; the admin can unsuspend.
	admin.Do("POST", "/v1/admin/resources/"+id+"/suspend", nil).Must(t, 200)
	if r := member.Do("POST", "/v1/resources/"+id+"/resume", nil); r.Status != 409 {
		t.Fatalf("owner resumed suspended: %s", r)
	}
	admin.Do("POST", "/v1/admin/resources/"+id+"/unsuspend", nil).Must(t, 200)
	e.WaitStatus(member, id, "active")
	// Audit rows exist for the lifecycle.
	audit := admin.Do("GET", "/v1/admin/audit?resourceId="+id, nil).Must(t, 200)
	seen := map[string]bool{}
	for _, a := range audit.Get("entries").([]any) {
		seen[a.(map[string]any)["action"].(string)] = true
	}
	for _, want := range []string{"resource.create", "resource.provisioned", "resource.plan_change", "resource.suspend", "resource.unsuspend"} {
		if !seen[want] {
			t.Errorf("missing audit action %s (have %v)", want, seen)
		}
	}
	// Project keys rotate.
	member.Do("POST", "/v1/projects/"+p.ID+"/keys/rotate", map[string]any{"kind": "secret"}).Must(t, 204)
	if r := e.ProjectClient(p.Secret).Do("GET", "/p/"+p.Ref, nil); r.Status != 401 {
		t.Fatalf("old secret key works: %s", r)
	}
}
