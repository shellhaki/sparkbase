//go:build e2e

// Package e2e drives a running Docker install end to end: through Traefik
// over TLS, with native Postgres (direct and pooled), Redis and S3 clients.
// Run with scripts/e2e-compose.sh.
package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/shellhaki/sparkbase/server/internal/s3"
)

var (
	domain    = env("E2E_DOMAIN", "sparkbase.localhost")
	httpsPort = env("E2E_HTTPS_PORT", "8443")
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// dial sends every *.<domain> name to the local Traefik.
func dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, _ := net.SplitHostPort(addr)
	if strings.HasSuffix(host, domain) {
		addr = net.JoinHostPort("127.0.0.1", port)
	}
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
}

func client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 60 * time.Second, Transport: &http.Transport{
		DialContext: dial, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // selfsigned mode
	}}
}

type api struct {
	t *testing.T
	c *http.Client
	h map[string]string
}

func (a *api) do(method, u string, body any) (int, map[string]any) {
	a.t.Helper()
	var rd io.Reader
	if b, ok := body.([]byte); ok {
		rd = bytes.NewReader(b)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range a.h {
		req.Header.Set(k, v)
	}
	res, err := a.c.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, u, err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	if res.StatusCode >= 400 {
		a.t.Logf("%s %s -> %d %s", method, u, res.StatusCode, data)
	}
	return res.StatusCode, out
}

func (a *api) must(status int, method, u string, body any) map[string]any {
	a.t.Helper()
	got, out := a.do(method, u, body)
	if got != status {
		a.t.Fatalf("%s %s: want %d got %d %v", method, u, status, got, out)
	}
	return out
}

func get(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func str(m map[string]any, path ...string) string { s, _ := get(m, path...).(string); return s }

func TestComposeInstall(t *testing.T) {
	ctx := context.Background()
	apiURL := fmt.Sprintf("https://api.%s:%s", domain, httpsPort)
	appURL := fmt.Sprintf("https://app.%s:%s", domain, httpsPort)
	a := &api{t: t, c: client()}

	// The API answers through Traefik; internal endpoints do not.
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if code, _ := a.do("GET", apiURL+"/health", nil); code == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("API never came up through Traefik")
		}
		time.Sleep(2 * time.Second)
	}
	if code, _ := a.do("GET", apiURL+"/internal/traefik/config?token=x", nil); code != 404 {
		t.Fatalf("internal endpoint reachable from outside: %d", code)
	}

	// Dashboard and its API proxy.
	dash := &api{t: t, c: client()}
	dash.must(200, "GET", appURL+"/api/v1/setup", nil)
	if code, _ := dash.do("GET", appURL+"/login", nil); code != 200 {
		t.Fatalf("dashboard login page: %d", code)
	}

	// First account becomes the admin.
	a.must(201, "POST", apiURL+"/v1/setup", map[string]any{"email": "admin@example.com", "password": "e2e password 123"})
	proj := a.must(201, "POST", apiURL+"/v1/projects", map[string]any{"name": "e2e"})
	pid, ref := str(proj, "project", "id"), str(proj, "project", "ref")
	ids := map[string]string{}
	for _, kind := range []string{"postgres", "cache", "storage", "vector", "queue", "auth"} {
		r := a.must(202, "POST", apiURL+"/v1/projects/"+pid+"/resources", map[string]any{"kind": kind, "name": map[string]string{"postgres": "main", "cache": "kv", "storage": "files", "vector": "docs", "queue": "jobs", "auth": "auth"}[kind]})
		ids[kind] = str(r, "resource", "id")
	}
	for kind, id := range ids {
		for i := 0; ; i++ {
			r := a.must(200, "GET", apiURL+"/v1/resources/"+id, nil)
			if s := str(r, "resource", "status"); s == "active" {
				break
			} else if s == "failed" || i > 120 {
				t.Fatalf("%s did not provision: %v", kind, r)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	keys := a.must(200, "GET", apiURL+"/v1/projects/"+pid+"/keys", nil)
	sec := &api{t: t, c: client(), h: map[string]string{"apikey": str(keys, "secretKey")}}
	base := apiURL + "/p/" + ref

	// Project API over HTTPS.
	sec.must(200, "POST", base+"/postgres/main/query", map[string]any{"sql": "create table notes (id serial primary key, body text)"})
	sec.must(200, "POST", base+"/postgres/main/query", map[string]any{"sql": "insert into notes (body) values ($1)", "params": []any{"hi"}})

	t.Run("native postgres through Traefik, direct and pooled", func(t *testing.T) {
		conn := a.must(200, "GET", apiURL+"/v1/resources/"+ids["postgres"]+"/connection", nil)
		for _, key := range []string{"url", "pooledUrl"} {
			u := str(conn, "native", key)
			if !strings.Contains(u, "sslmode=require") || !strings.Contains(u, ".db."+domain) {
				t.Fatalf("%s: %s", key, u)
			}
			cfg, err := pgx.ParseConfig(u)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TLSConfig.ServerName = cfg.Host
			cfg.LookupFunc = func(ctx context.Context, host string) ([]string, error) { return []string{"127.0.0.1"}, nil }
			cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
			// New services are routed after Traefik's next poll (5 s).
			var c *pgx.Conn
			for i := 0; ; i++ {
				c, err = pgx.ConnectConfig(ctx, cfg)
				if err == nil {
					break
				}
				if i > 30 {
					t.Fatalf("%s connect: %v", key, err)
				}
				time.Sleep(time.Second)
			}
			var body string
			if err := c.QueryRow(ctx, "select body from notes limit 1").Scan(&body); err != nil || body != "hi" {
				t.Fatalf("%s query: %q %v", key, body, err)
			}
			c.Close(ctx)
		}
		// Without TLS the connection is refused.
		u := str(conn, "native", "url")
		cfg, _ := pgx.ParseConfig(strings.Replace(u, "sslmode=require", "sslmode=disable", 1))
		cfg.LookupFunc = func(ctx context.Context, host string) ([]string, error) { return []string{"127.0.0.1"}, nil }
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if c, err := pgx.ConnectConfig(cctx, cfg); err == nil {
			c.Close(ctx)
			t.Fatal("plaintext connection accepted")
		}
	})

	t.Run("pgvector is available", func(t *testing.T) {
		sec.must(201, "POST", base+"/vector/docs/collections", map[string]any{"name": "pages", "dimension": 3})
		sec.must(200, "POST", base+"/vector/docs/collections/pages/upsert", map[string]any{"items": []any{map[string]any{"id": "a", "vector": []float64{1, 0, 0}}}})
		r := sec.must(200, "POST", base+"/vector/docs/collections/pages/query", map[string]any{"vector": []float64{1, 0, 0}})
		if fmt.Sprint(get(r, "matches")) == "[]" {
			t.Fatal("no vector match")
		}
		vr := a.must(200, "GET", apiURL+"/v1/resources/"+ids["vector"], nil)
		if str(vr, "resource", "runtime") != "container" {
			t.Fatalf("vector runtime: %v", vr)
		}
	})

	t.Run("native redis over TLS", func(t *testing.T) {
		conn := a.must(200, "GET", apiURL+"/v1/resources/"+ids["cache"]+"/connection", nil)
		opt, err := redis.ParseURL(str(conn, "native", "url"))
		if err != nil {
			t.Fatal(err)
		}
		host, _, _ := net.SplitHostPort(opt.Addr)
		opt.TLSConfig = &tls.Config{ServerName: host, InsecureSkipVerify: true}
		opt.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := dial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return tls.Client(c, opt.TLSConfig), nil
		}
		opt.DisableIdentity = true
		rc := redis.NewClient(opt)
		defer rc.Close()
		prefix := str(conn, "native", "keyPrefix")
		for i := 0; ; i++ {
			err := rc.Set(ctx, prefix+"k", "v", 0).Err()
			if err == nil {
				break
			}
			if i > 30 {
				t.Fatalf("native set: %v", err)
			}
			time.Sleep(time.Second)
		}
		if err := rc.Set(ctx, "outside", "v", 0).Err(); err == nil {
			t.Fatal("wrote outside the prefix")
		}
		got := sec.must(200, "GET", base+"/cache/kv/keys/k", nil)
		if str(got, "value") != "v" {
			t.Fatalf("API read of native write: %v", got)
		}
	})

	t.Run("storage through Garage and s3.<domain>", func(t *testing.T) {
		sec.must(201, "PUT", base+"/storage/files/objects/hello.txt", []byte("hello compose"))
		signed := sec.must(200, "POST", base+"/storage/files/sign", map[string]any{"key": "hello.txt"})
		u := str(signed, "url")
		if !strings.HasPrefix(u, "https://s3."+domain) {
			t.Fatalf("presigned URL host: %s", u)
		}
		res, err := client().Get(u)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || string(body) != "hello compose" {
			t.Fatalf("presigned get: %d %s", res.StatusCode, body)
		}
		conn := a.must(200, "GET", apiURL+"/v1/resources/"+ids["storage"]+"/connection", nil)
		c := &s3.Client{Endpoint: str(conn, "native", "endpoint"), Region: str(conn, "native", "region"),
			AccessKey: str(conn, "native", "accessKeyId"), SecretKey: str(conn, "native", "secretAccessKey"), HTTP: client()}
		objs, _, err := c.List(ctx, str(conn, "native", "bucket"), "", "", 10)
		if err != nil || len(objs) != 1 {
			t.Fatalf("native S3 list: %v %v", objs, err)
		}
		pu, _ := url.Parse(u)
		_ = pu
	})

	t.Run("queue and auth", func(t *testing.T) {
		sec.must(201, "POST", base+"/queue/jobs/queues", map[string]any{"name": "q"})
		sec.must(201, "POST", base+"/queue/jobs/queues/q/send", map[string]any{"messages": []any{map[string]any{"body": 1}}})
		pub := &api{t: t, c: client(), h: map[string]string{"apikey": str(keys, "publishableKey")}}
		s := pub.must(201, "POST", base+"/auth/signup", map[string]any{"email": "u@example.com", "password": "user password 1"})
		if str(s, "session", "access_token") == "" {
			t.Fatal("no session")
		}
	})

	t.Run("backup runs inside the server image", func(t *testing.T) {
		a.must(202, "POST", apiURL+"/v1/resources/"+ids["postgres"]+"/backups", nil)
		for i := 0; ; i++ {
			list := a.must(200, "GET", apiURL+"/v1/resources/"+ids["postgres"]+"/backups", nil)
			b := list["backups"].([]any)[0].(map[string]any)
			if b["status"] == "done" {
				break
			}
			if b["status"] == "failed" || i > 60 {
				t.Fatalf("backup: %v", b)
			}
			time.Sleep(500 * time.Millisecond)
		}
	})

	t.Run("idle containers stop and wake on connect", func(t *testing.T) {
		a.must(200, "PATCH", apiURL+"/v1/admin/settings", map[string]any{"idleMinutes": 1})
		conn := a.must(200, "GET", apiURL+"/v1/resources/"+ids["postgres"]+"/connection", nil)
		cfg, _ := pgx.ParseConfig(str(conn, "native", "url"))
		cfg.TLSConfig.ServerName = cfg.Host
		cfg.LookupFunc = func(ctx context.Context, host string) ([]string, error) { return []string{"127.0.0.1"}, nil }
		// Wait for the reaper: idle for over a minute, checked every minute.
		deadline := time.Now().Add(4 * time.Minute)
		for {
			m := a.must(200, "GET", apiURL+"/v1/resources/"+ids["postgres"]+"/metrics?minutes=5", nil)
			if m["awake"] == false {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the container never went to sleep")
			}
			time.Sleep(5 * time.Second)
		}
		start := time.Now()
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		c, err := pgx.ConnectConfig(cctx, cfg)
		if err != nil {
			t.Fatalf("wake on connect: %v", err)
		}
		c.Close(ctx)
		t.Logf("cold start through Traefik: %s", time.Since(start).Round(time.Millisecond))
	})

	t.Run("pause removes the Traefik route", func(t *testing.T) {
		a.must(200, "POST", apiURL+"/v1/resources/"+ids["postgres"]+"/pause", nil)
		conn := a.must(200, "GET", apiURL+"/v1/resources/"+ids["postgres"]+"/connection", nil)
		cfg, _ := pgx.ParseConfig(str(conn, "native", "url"))
		cfg.TLSConfig.ServerName = cfg.Host
		cfg.LookupFunc = func(ctx context.Context, host string) ([]string, error) { return []string{"127.0.0.1"}, nil }
		time.Sleep(7 * time.Second) // one Traefik poll
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if c, err := pgx.ConnectConfig(cctx, cfg); err == nil {
			c.Close(ctx)
			t.Fatal("paused database still reachable")
		}
	})

	a.must(202, "DELETE", apiURL+"/v1/projects/"+pid, nil)
}
