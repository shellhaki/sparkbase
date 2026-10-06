package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/store"
	"github.com/shellhaki/sparkbase/server/internal/testenv"
	"github.com/shellhaki/sparkbase/server/internal/worker"
)

func TestJobsClaimRetryAndStuck(t *testing.T) {
	e := testenv.New(t)
	ctx := context.Background()
	st := e.App.Store

	// Two workers never take the same job.
	var runs sync.Map
	var dup atomic.Bool
	w1, w2 := worker.New(e.App), worker.New(e.App)
	for _, w := range []*worker.Worker{w1, w2} {
		w.Handle("test_once", func(ctx context.Context, p json.RawMessage) error {
			var v struct{ N int }
			_ = json.Unmarshal(p, &v)
			if _, loaded := runs.LoadOrStore(v.N, true); loaded {
				dup.Store(true)
			}
			return nil
		})
	}
	for i := 0; i < 50; i++ {
		if _, err := st.Enqueue(ctx, store.NewJob{Type: "test_once", Payload: map[string]int{"N": i}}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, w := range []*worker.Worker{w1, w2} {
		wg.Add(1)
		go func(w *worker.Worker) { defer wg.Done(); _ = w.Drain(ctx) }(w)
	}
	wg.Wait()
	n := 0
	runs.Range(func(_, _ any) bool { n++; return true })
	if dup.Load() || n != 50 {
		t.Fatalf("ran %d jobs, duplicate=%v", n, dup.Load())
	}

	// A failing job retries, then ends failed with its error saved.
	w := worker.New(e.App)
	w.Handle("test_fail", func(context.Context, json.RawMessage) error { return errors.New("boom") })
	var hooked atomic.Bool
	w.OnFail("test_fail", func(context.Context, json.RawMessage, error) { hooked.Store(true) })
	id, _ := st.Enqueue(ctx, store.NewJob{Type: "test_fail", MaxAttempts: 2})
	for i := 0; i < 2; i++ {
		if _, err := e.App.Store.Pool.Exec(ctx, `update jobs set run_at = now() where id = $1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := w.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	job, _ := st.GetJob(ctx, id)
	if job.Status != "failed" || job.LastError == nil || *job.LastError != "boom" || !hooked.Load() {
		t.Fatalf("job after retries: %+v hooked=%v", job, hooked.Load())
	}

	// A job left running by a crashed worker returns to the queue.
	id2, _ := st.Enqueue(ctx, store.NewJob{Type: "test_stuck"})
	if _, err := st.Pool.Exec(ctx, `update jobs set status = 'running', locked_at = now() - interval '1 hour' where id = $1`, id2); err != nil {
		t.Fatal(err)
	}
	if n, err := st.RequeueStuck(ctx, time.Minute); err != nil || n != 1 {
		t.Fatalf("requeue: %d %v", n, err)
	}
	job, _ = st.GetJob(ctx, id2)
	if job.Status != "queued" {
		t.Fatalf("stuck job: %+v", job)
	}

	// Dedupe keys keep one queued copy.
	a, _ := st.Enqueue(ctx, store.NewJob{Type: "x", DedupeKey: "k"})
	b, _ := st.Enqueue(ctx, store.NewJob{Type: "x", DedupeKey: "k"})
	if a == 0 || b != 0 {
		t.Fatalf("dedupe: %d %d", a, b)
	}
}

func TestBackupAndRestore(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	p := e.Project(admin, "bk")
	id := e.Service(admin, p, "postgres", "main")
	api := e.ProjectClient(p.Secret)
	q := func(sql string) *testenv.Resp {
		return api.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": sql}).Must(t, 200)
	}
	q("create table notes (id int primary key, body text)")
	q("insert into notes values (1, 'keep me')")

	b := admin.Do("POST", "/v1/resources/"+id+"/backups", nil).Must(t, 202)
	bid := b.Str("backup", "id")
	deadline := time.Now().Add(30 * time.Second)
	for {
		list := admin.Do("GET", "/v1/resources/"+id+"/backups", nil).Must(t, 200)
		status := list.Str("backups", "0", "status")
		if status == "done" {
			break
		}
		if status == "failed" || time.Now().After(deadline) {
			t.Fatalf("backup: %s", list)
		}
		time.Sleep(100 * time.Millisecond)
	}
	q("delete from notes")
	q("insert into notes values (2, 'after backup')")
	admin.Do("POST", "/v1/resources/"+id+"/backups/"+bid+"/restore", nil).Must(t, 204)
	rows := q("select id, body from notes order by id")
	if len(rows.Get("rows").([]any)) != 1 || rows.Str("rows", "0", "body") != "keep me" {
		t.Fatalf("restore: %s", rows)
	}
	// Restored tables still belong to the tenant: writes work.
	q("insert into notes values (3, 'writable')")
}

func TestTraefikConfigAndStatus(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	p := e.Project(admin, "edge")
	id := e.Service(admin, p, "postgres", "main")
	e.Service(admin, p, "cache", "kv")

	if r := e.Client().Do("GET", "/internal/traefik/config", nil); r.Status != 401 {
		t.Fatalf("config without token: %s", r)
	}
	tc := e.Client().WithHeader("Authorization", "Bearer traefik-test-token")
	cfg := tc.Do("GET", "/internal/traefik/config", nil).Must(t, 200)
	routers := cfg.Get("tcp", "routers").(map[string]any)
	if len(routers) != 3 { // direct + pooled postgres, and redis
		t.Fatalf("routers: %s", cfg)
	}
	found := false
	for _, r := range routers {
		if r.(map[string]any)["rule"] == "HostSNI(`main-"+p.Ref+".db.localhost`)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no router for the database: %s", cfg)
	}
	admin.Do("POST", "/v1/resources/"+id+"/pause", nil).Must(t, 200)
	cfg = tc.Do("GET", "/internal/traefik/config", nil).Must(t, 200)
	if n := len(cfg.Get("tcp", "routers").(map[string]any)); n != 1 {
		t.Fatalf("paused service still routed: %s", cfg)
	}

	status := admin.Do("GET", "/v1/status", nil).Must(t, 200)
	if status.Get("healthy") != true || len(status.Get("hosts").([]any)) != 3 {
		t.Fatalf("status: %s", status)
	}
	metrics := tc.Do("GET", "/metrics", nil).Must(t, 200)
	if len(metrics.Raw) == 0 {
		t.Fatal("empty metrics")
	}
}
