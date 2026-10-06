package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/engine"
	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

func TestContainerServicesScaleToZero(t *testing.T) {
	e := testenv.New(t, testenv.WithContainers())
	ctx := context.Background()
	admin := e.Admin()
	p := e.Project(admin, "containers")
	id := e.Service(admin, p, "postgres", "main")
	api := e.ProjectClient(p.Secret)
	q := func(sql string) *testenv.Resp {
		return api.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": sql})
	}
	q("create table t (v int)").Must(t, 200)
	q("insert into t values (1)").Must(t, 200)

	r, err := e.App.Store.GetResource(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Runtime != "container" {
		t.Fatalf("runtime: %s", r.Runtime)
	}
	t.Cleanup(func() { _ = e.App.Engine.Destroy(context.Background(), r) })

	// The plan's memory limit applies to the container.
	if st, err := e.App.Engine.Docker.Stats(ctx, engine.ContainerName(r)); err != nil || st.MemoryLimit != 256<<20 {
		t.Fatalf("memory limit: %+v %v", st, err)
	}

	// Native connections go through the activator.
	url := nativeURL(t, admin, id)
	time.Sleep(1500 * time.Millisecond) // the activator picks up new services every second
	c, err := connect(ctx, url)
	if err != nil {
		t.Fatalf("native through activator: %v", err)
	}
	c.Close(ctx)

	sleep := func() {
		t.Helper()
		e.App.Tenants.Close()
		if _, err := e.App.Store.Pool.Exec(ctx, `update resources set last_active_at = now() - interval '1 hour' where id = $1`, id); err != nil {
			t.Fatal(err)
		}
		// Let the activator release its connection counter.
		time.Sleep(300 * time.Millisecond)
		if err := e.App.ScaleToZero(ctx); err != nil {
			t.Fatal(err)
		}
		if running, _ := e.App.Engine.Running(ctx, r); running {
			t.Fatal("idle container still running")
		}
	}

	t.Run("an API call wakes a sleeping service", func(t *testing.T) {
		sleep()
		start := time.Now()
		res := q("select v from t").Must(t, 200)
		if res.Get("rows", "0", "v").(float64) != 1 {
			t.Fatalf("data after wake: %s", res)
		}
		t.Logf("cold start through the API: %s", time.Since(start).Round(time.Millisecond))
	})

	t.Run("a native connection wakes a sleeping service", func(t *testing.T) {
		sleep()
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		c, err := connect(cctx, url)
		if err != nil {
			t.Fatalf("native wake: %v", err)
		}
		var v int
		if err := c.QueryRow(cctx, "select v from t").Scan(&v); err != nil || v != 1 {
			t.Fatalf("native query: %d %v", v, err)
		}
		c.Close(ctx)
	})

	t.Run("busy services are not stopped", func(t *testing.T) {
		c, err := connect(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(ctx)
		e.App.Store.Pool.Exec(ctx, `update resources set last_active_at = now() - interval '1 hour' where id = $1`, id)
		if err := e.App.ScaleToZero(ctx); err != nil {
			t.Fatal(err)
		}
		if running, _ := e.App.Engine.Running(ctx, r); !running {
			t.Fatal("stopped a container with an open connection")
		}
	})

	t.Run("paused services stay down", func(t *testing.T) {
		admin.Do("POST", "/v1/resources/"+id+"/pause", nil).Must(t, 200)
		if running, _ := e.App.Engine.Running(ctx, r); running {
			t.Fatal("paused container still running")
		}
		if r := q("select 1"); r.Code() != "resource_not_ready" {
			t.Fatalf("paused API: %s", r)
		}
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if c, err := connect(cctx, url); err == nil {
			c.Close(ctx)
			t.Fatal("paused service accepted a native connection")
		}
		admin.Do("POST", "/v1/resources/"+id+"/resume", nil).Must(t, 200)
		q("select 1").Must(t, 200)
	})

	t.Run("cache in its own container", func(t *testing.T) {
		cid := e.Service(admin, p, "cache", "kv")
		cr, _ := e.App.Store.GetResource(ctx, cid)
		time.Sleep(1500 * time.Millisecond)
		t.Cleanup(func() { _ = e.App.Engine.Destroy(context.Background(), cr) })
		api.Do("PUT", "/p/"+p.Ref+"/cache/kv/keys/a", map[string]any{"value": "1"}).Must(t, 200)
		if got := api.Do("GET", "/p/"+p.Ref+"/cache/kv/keys/a", nil).Must(t, 200).Str("value"); got != "1" {
			t.Fatalf("cache value: %q", got)
		}
		rc, _ := redisAs(t, admin, cid)
		if err := rc.Ping(ctx).Err(); err != nil {
			t.Fatalf("native redis through activator: %v", err)
		}
	})

	t.Run("delete removes the container", func(t *testing.T) {
		admin.Do("DELETE", "/v1/resources/"+id, nil).Must(t, 202)
		e.WaitGone(admin, id)
		st, _ := e.App.Engine.Docker.Inspect(ctx, engine.ContainerName(r))
		if st.Exists {
			t.Fatal("container still exists after delete")
		}
	})
}
