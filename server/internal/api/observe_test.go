package api_test

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

func TestLogsMetricsAndConsoles(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	p := e.Project(admin, "obs")
	pgID := e.Service(admin, p, "postgres", "main")
	cacheID := e.Service(admin, p, "cache", "kv")
	api := e.ProjectClient(p.Secret)

	// Start a live tail before generating traffic.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.URL+"/v1/resources/"+pgID+"/logs?follow=1", nil)
	live, err := admin.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Body.Close()
	if ct := live.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	lines := make(chan string, 100)
	go func() {
		sc := bufio.NewScanner(live.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	api.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select 1"}).Must(t, 200)
	api.Do("POST", "/p/"+p.Ref+"/postgres/main/query", map[string]any{"sql": "select * from nope"})

	got := ""
	deadline := time.After(10 * time.Second)
	for !strings.Contains(got, "nope") && !strings.Contains(got, "query_failed") {
		select {
		case l := <-lines:
			got += l + "\n"
		case <-deadline:
			t.Fatalf("live tail never showed the failing query:\n%s", got)
		}
	}
	if !strings.Contains(got, "POST /postgres/main/query") {
		t.Fatalf("live tail: %s", got)
	}

	// Backlog without follow.
	backlog := admin.Do("GET", "/v1/resources/"+pgID+"/logs", nil).Must(t, 200)
	if n := len(backlog.Get("entries").([]any)); n < 2 {
		t.Fatalf("backlog: %d", n)
	}

	// Per-minute metrics count requests and errors.
	m := admin.Do("GET", "/v1/resources/"+pgID+"/metrics?minutes=5", nil).Must(t, 200)
	pts := m.Get("requests").([]any)
	last := pts[len(pts)-1].(map[string]any)
	if last["requests"].(float64) < 2 || last["errors"].(float64) < 1 {
		t.Fatalf("metrics: %v", last)
	}

	// Redis console, with automatic key prefix and ACL limits.
	cmd := func(args ...string) string {
		return api.Do("POST", "/p/"+p.Ref+"/cache/kv/command", map[string]any{"args": args}).Must(t, 200).Str("output")
	}
	if out := cmd("SET", "greeting", "hello"); out != `"OK"` {
		t.Fatalf("set: %s", out)
	}
	if out := cmd("GET", "greeting"); out != `"hello"` {
		t.Fatalf("get: %s", out)
	}
	if out := cmd("FLUSHALL"); !strings.HasPrefix(out, "(error)") {
		t.Fatalf("flushall allowed: %s", out)
	}
	if out := api.Do("GET", "/p/"+p.Ref+"/cache/kv/keys/greeting", nil).Must(t, 200).Str("value"); out != "hello" {
		t.Fatalf("console key not under prefix: %s", out)
	}
	_ = cacheID

	ext := api.Do("GET", "/p/"+p.Ref+"/postgres/main/extensions", nil).Must(t, 200)
	if !strings.Contains(string(ext.Raw), "pgcrypto") {
		t.Fatalf("extensions: %s", ext)
	}

	// Members cannot read system logs; admins can, without secrets.
	member := e.Invite(admin, "m@example.com", "member")
	if r := member.Do("GET", "/v1/admin/logs", nil); r.Status != 403 {
		t.Fatalf("member read system logs: %s", r)
	}
	e.App.Log.Info("test line", "password", "hunter2", "target", "postgres://u:s3cret@h/db")
	time.Sleep(200 * time.Millisecond)
	sys := admin.Do("GET", "/v1/admin/logs", nil).Must(t, 200)
	if strings.Contains(string(sys.Raw), "hunter2") || strings.Contains(string(sys.Raw), "s3cret") || !strings.Contains(string(sys.Raw), "test line") {
		t.Fatalf("system logs: %s", sys.Raw)
	}
}
