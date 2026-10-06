package api_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

// sseLines opens an SSE stream and returns its lines on a channel.
func sseLines(t *testing.T, ctx context.Context, c *http.Client, url string, headers map[string]string) (<-chan string, int) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Logf("sse %s: %d %s", url, res.StatusCode, b)
		return nil, res.StatusCode
	}
	out := make(chan string, 100)
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			out <- sc.Text()
		}
		close(out)
	}()
	return out, 200
}

func waitFor(t *testing.T, ch <-chan string, needle string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	seen := ""
	for {
		select {
		case l, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed before %q; saw:\n%s", needle, seen)
			}
			seen += l + "\n"
			if strings.Contains(l, needle) {
				return
			}
		case <-deadline:
			t.Fatalf("never saw %q; saw:\n%s", needle, seen)
		}
	}
}

func TestRealtime(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	p := e.Project(admin, "rt")
	e.Service(admin, p, "realtime", "live")
	e.Service(admin, p, "postgres", "main")
	sec := e.ProjectClient(p.Secret)
	base := e.URL + "/p/" + p.Ref + "/realtime/live"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Browsers subscribe to public channels with the publishable key in the URL.
	pub, code := sseLines(t, ctx, http.DefaultClient, base+"/subscribe?channel=public:room1&apikey="+p.Publishable, nil)
	if code != 200 {
		t.Fatal("publishable subscribe failed")
	}
	waitFor(t, pub, "event: ready")
	if _, code := sseLines(t, ctx, http.DefaultClient, base+"/subscribe?channel=private&apikey="+p.Publishable, nil); code != 403 {
		t.Fatalf("publishable key joined a private channel: %d", code)
	}
	if r := e.ProjectClient(p.Publishable).Do("POST", "/p/"+p.Ref+"/realtime/live/broadcast", map[string]any{"channel": "public:room1", "payload": 1}); r.Status != 403 {
		t.Fatalf("publishable key broadcast: %s", r)
	}
	sec.Do("POST", "/p/"+p.Ref+"/realtime/live/broadcast", map[string]any{"channel": "public:room1", "event": "chat", "payload": map[string]any{"text": "hi"}}).Must(t, 200)
	waitFor(t, pub, `"text":"hi"`)

	// Table changes.
	sec.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "create table todos (id serial primary key, title text)"}).Must(t, 200)
	sec.Do("POST", "/p/"+p.Ref+"/realtime/live/watches", map[string]any{"postgres": "main", "table": "todos"}).Must(t, 201)
	db, code := sseLines(t, ctx, http.DefaultClient, base+"/subscribe?channel=db:main:todos", map[string]string{"apikey": p.Secret})
	if code != 200 {
		t.Fatal("secret subscribe failed")
	}
	waitFor(t, db, "event: ready")
	time.Sleep(500 * time.Millisecond) // listener connects
	sec.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "insert into todos (title) values ('watch me')"}).Must(t, 200)
	waitFor(t, db, "watch me")
	w := sec.Do("GET", "/p/"+p.Ref+"/realtime/live/watches", nil).Must(t, 200)
	if w.Get("subscribers").(float64) < 1 {
		t.Fatalf("subscribers: %s", w)
	}
	sec.Do("DELETE", "/p/"+p.Ref+"/realtime/live/watches/main/todos", nil).Must(t, 204)
}

func TestCronMonitorAndBackupServices(t *testing.T) {
	e := testenv.New(t)
	ctx := context.Background()
	admin := e.Admin()
	p := e.Project(admin, "ctl")
	e.Service(admin, p, "postgres", "main")
	e.Service(admin, p, "queue", "jobs")
	cronID := e.Service(admin, p, "cron", "tasks")
	monID := e.Service(admin, p, "monitor", "uptime")
	e.Service(admin, p, "backup", "nightly")
	sec := e.ProjectClient(p.Secret)
	pre := "/p/" + p.Ref

	// Platform services have no credentials; connection info still answers.
	if got := admin.Do("GET", "/v1/resources/"+cronID+"/connection", nil).Must(t, 200).Str("kind"); got != "cron" {
		t.Fatalf("connection kind = %q", got)
	}

	var hits atomic.Int64
	var healthy atomic.Bool
	healthy.Store(true)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !healthy.Load() {
			w.WriteHeader(503)
		}
	}))
	defer target.Close()

	t.Run("private targets are blocked by default", func(t *testing.T) {
		j := sec.Do("POST", pre+"/cron/tasks/jobs", map[string]any{"name": "ping", "schedule": "* * * * *",
			"action": map[string]any{"type": "http", "url": target.URL}}).Must(t, 201)
		r := sec.Do("POST", pre+"/cron/tasks/jobs/"+j.Str("job", "id")+"/run", nil).Must(t, 200)
		if r.Str("status") != "failed" || !strings.Contains(r.Str("output"), "private network") || hits.Load() != 0 {
			t.Fatalf("private target reached: %s", r)
		}
	})
	admin.Do("PATCH", "/v1/admin/settings", map[string]any{"allowPrivateTargets": true}).Must(t, 200)

	t.Run("cron jobs", func(t *testing.T) {
		if r := sec.Do("POST", pre+"/cron/tasks/jobs", map[string]any{"name": "bad", "schedule": "nope", "action": map[string]any{"type": "http", "url": target.URL}}); r.Status != 422 {
			t.Fatalf("bad schedule: %s", r)
		}
		sec.Do("POST", pre+"/postgres/main/query", map[string]any{"sql": "create table ticks (at timestamptz default now())"}).Must(t, 200)
		sec.Do("POST", pre+"/queue/jobs/queues", map[string]any{"name": "emails"}).Must(t, 201)
		sqlJob := sec.Do("POST", pre+"/cron/tasks/jobs", map[string]any{"name": "tick", "schedule": "*/5 * * * *",
			"action": map[string]any{"type": "sql", "postgres": "main", "sql": "insert into ticks default values"}}).Must(t, 201)
		sec.Do("POST", pre+"/cron/tasks/jobs", map[string]any{"name": "enqueue", "schedule": "0 * * * *",
			"action": map[string]any{"type": "queue", "queue": "jobs", "queueName": "emails", "body": map[string]any{"to": "a@example.com"}}}).Must(t, 201)

		// Make every job due, then run the scheduler once.
		e.App.Store.Pool.Exec(ctx, `update cron_jobs set next_run_at = now() - interval '1 minute' where resource_id = $1`, cronID)
		if err := e.App.CronTick(ctx); err != nil {
			t.Fatal(err)
		}
		n := sec.Do("POST", pre+"/postgres/main/query", map[string]any{"sql": "select count(*)::int as n from ticks"}).Must(t, 200)
		if n.Get("rows", "0", "n").(float64) != 1 {
			t.Fatalf("sql job did not run: %s", n)
		}
		st := sec.Do("GET", pre+"/queue/jobs/queues/emails/stats", nil).Must(t, 200)
		if st.Get("ready").(float64) != 1 {
			t.Fatalf("queue job did not run: %s", st)
		}
		if hits.Load() != 1 {
			t.Fatalf("http job hits: %d", hits.Load())
		}
		runs := sec.Do("GET", pre+"/cron/tasks/jobs/"+sqlJob.Str("job", "id")+"/runs", nil).Must(t, 200)
		if runs.Str("runs", "0", "status") != "ok" {
			t.Fatalf("runs: %s", runs)
		}
		jobs := sec.Do("GET", pre+"/cron/tasks/jobs", nil).Must(t, 200)
		for _, j := range jobs.Get("jobs").([]any) {
			next, _ := time.Parse(time.RFC3339, j.(map[string]any)["nextRunAt"].(string))
			if !next.After(time.Now()) {
				t.Fatalf("next run not moved forward: %v", j)
			}
		}
		// Cron runs show up in the service's log.
		lg := admin.Do("GET", "/v1/resources/"+cronID+"/logs", nil).Must(t, 200)
		if !strings.Contains(string(lg.Raw), "tick ok") {
			t.Fatalf("cron log: %s", lg.Raw)
		}
	})

	t.Run("uptime checks", func(t *testing.T) {
		c := sec.Do("POST", pre+"/monitor/uptime/checks", map[string]any{"name": "site", "url": target.URL}).Must(t, 201)
		id := c.Str("check", "id")
		r := sec.Do("POST", pre+"/monitor/uptime/checks/"+id+"/run", nil).Must(t, 200)
		if r.Get("result", "ok") != true {
			t.Fatalf("check: %s", r)
		}
		healthy.Store(false)
		e.App.Store.Pool.Exec(ctx, `update monitors set next_check_at = now() - interval '1 minute' where resource_id = $1`, monID)
		if err := e.App.MonitorTick(ctx); err != nil {
			t.Fatal(err)
		}
		list := sec.Do("GET", pre+"/monitor/uptime/checks", nil).Must(t, 200)
		if list.Str("checks", "0", "check", "state") != "down" || list.Get("checks", "0", "uptime24h").(float64) != 0.5 {
			t.Fatalf("after failure: %s", list)
		}
		notes := admin.Do("GET", "/v1/notifications", nil).Must(t, 200)
		if !strings.Contains(string(notes.Raw), "site is DOWN") {
			t.Fatalf("no down alert: %s", notes.Raw)
		}
	})

	t.Run("backup service", func(t *testing.T) {
		sec.Do("PATCH", pre+"/backup/nightly/settings", map[string]any{"targets": []string{"main", "jobs"}, "retentionDays": 3, "schedule": "0 2 * * *"}).Must(t, 200)
		if r := sec.Do("PATCH", pre+"/backup/nightly/settings", map[string]any{"schedule": "* * * * *"}); r.Status != 422 {
			t.Fatalf("too-frequent schedule: %s", r)
		}
		if r := sec.Do("PATCH", pre+"/backup/nightly/settings", map[string]any{"retentionDays": 365}); r.Status != 422 {
			t.Fatalf("retention above plan: %s", r)
		}
		sec.Do("POST", pre+"/backup/nightly/run", nil).Must(t, 202)
		deadline := time.Now().Add(30 * time.Second)
		for {
			runs := sec.Do("GET", pre+"/backup/nightly/runs", nil).Must(t, 200)
			list := runs.Get("runs").([]any)
			done := 0
			for _, x := range list {
				if x.(map[string]any)["backup"].(map[string]any)["status"] == "done" {
					done++
				}
			}
			if done == 2 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("backups did not finish: %s", runs)
			}
			time.Sleep(200 * time.Millisecond)
		}
		// S3 secrets are never returned.
		sec.Do("PATCH", pre+"/backup/nightly/settings", map[string]any{"destination": map[string]any{"type": "s3", "endpoint": "https://s3.example.com", "bucket": "b", "accessKey": "AK", "secretKey": "very-secret"}}).Must(t, 200)
		got := sec.Do("GET", pre+"/backup/nightly/settings", nil).Must(t, 200)
		if strings.Contains(string(got.Raw), "very-secret") || got.Get("settings", "hasSecret") != true {
			t.Fatalf("settings leak: %s", got.Raw)
		}
	})
}
