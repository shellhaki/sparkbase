// Package testenv starts a full Sparkbase (API + worker) against the local
// dev services for integration tests. Each Env gets its own control
// database, Redis prefix and storage directory.
package testenv

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shellhaki/sparkbase/server/internal/api"
	"github.com/shellhaki/sparkbase/server/internal/app"
	"github.com/shellhaki/sparkbase/server/internal/config"
	"github.com/shellhaki/sparkbase/server/internal/logx"
	"github.com/shellhaki/sparkbase/server/internal/mail"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/worker"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var (
	ControlAdminURL = envOr("SB_TEST_CONTROL_URL", "postgres://sparkbase:sparkbase@127.0.0.1:54329/postgres?sslmode=disable")
	DataHostURL     = envOr("SB_TEST_DATA_HOST", "postgres://sb_admin:devadmin@127.0.0.1:54330/postgres")
	RedisURL        = envOr("SB_TEST_REDIS_URL", "redis://:devredis@127.0.0.1:63799/0")
	CacheHostURL    = envOr("SB_TEST_CACHE_HOST", "redis://:devredis@127.0.0.1:63799")
)

type Env struct {
	T      *testing.T
	App    *app.App
	API    *api.Server
	Worker *worker.Worker
	Server *httptest.Server
	Mail   *mail.Capture
	URL    string
	dbName string
}

// New starts an Env, or skips the test when dev services are not running.
func New(t *testing.T) *Env {
	t.Helper()
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, ControlAdminURL)
	if err != nil {
		if os.Getenv("SB_REQUIRE_SERVICES") == "1" {
			t.Fatalf("dev services are not running: %v", err)
		}
		t.Skip("dev services are not running; start them with scripts/dev-services.sh")
	}
	dbName := "sbt_" + sbcrypto.RandomLower(10)
	if _, err := admin.Exec(ctx, "create database "+dbName); err != nil {
		t.Fatal(err)
	}
	admin.Close(ctx)

	var handler atomic.Pointer[http.Handler]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		(*handler.Load()).ServeHTTP(w, r)
	}))

	controlURL := strings.Replace(ControlAdminURL, "/postgres?", "/"+dbName+"?", 1)
	key := base64.StdEncoding.EncodeToString(sbcrypto.RandomBytes(32))
	vars := map[string]string{
		"SPARKBASE_DOMAIN":     "localhost",
		"PUBLIC_API_URL":       srv.URL,
		"DASHBOARD_URL":        "http://localhost:3000",
		"DATABASE_URL":         controlURL,
		"REDIS_URL":            RedisURL,
		"REDIS_PREFIX":         "sbt:" + sbcrypto.RandomLower(8) + ":",
		"ENCRYPTION_KEY":       key,
		"TRAEFIK_CONFIG_TOKEN": "traefik-test-token",
		"NATIVE_MODE":          "direct",
		"TLS_MODE":             "none",
		"BOOTSTRAP_PG_HOST":    DataHostURL,
		"BOOTSTRAP_CACHE_HOST": CacheHostURL,
		"BOOTSTRAP_STORAGE":    "local://" + t.TempDir(),
		"BACKUP_DIR":           t.TempDir(),
	}
	cfg, err := config.Load(func(k string) string { return vars[k] })
	if err != nil {
		t.Fatal(err)
	}
	capture := &mail.Capture{}
	log := logx.Discard()
	if os.Getenv("SB_TEST_LOG") == "1" {
		log = logx.New(os.Stderr, "debug")
	}
	a, err := app.New(ctx, cfg, log, capture)
	if err != nil {
		t.Fatal(err)
	}
	s := api.New(a)
	h := s.Handler()
	handler.Store(&h)

	w := worker.New(a)
	w.PollInterval = 20 * time.Millisecond
	w.Concurrency = 2
	wctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { w.Run(wctx); close(done) }()

	e := &Env{T: t, App: a, API: s, Worker: w, Server: srv, Mail: capture, URL: srv.URL, dbName: dbName}
	t.Cleanup(func() {
		cancel()
		<-done
		e.teardownTenants()
		srv.Close()
		a.Close()
		if c, err := pgx.Connect(context.Background(), ControlAdminURL); err == nil {
			c.Exec(context.Background(), "drop database if exists "+dbName+" with (force)")
			c.Close(context.Background())
		}
	})
	return e
}

// teardownTenants removes every tenant database, role and ACL user the test created.
func (e *Env) teardownTenants() {
	ctx := context.Background()
	list, err := e.App.Store.ListAllResources(ctx)
	if err != nil {
		return
	}
	for _, r := range list {
		if err := e.App.DeprovisionResource(ctx, r.ID); err != nil {
			e.T.Logf("teardown %s %s: %v", r.Kind, r.ID, err)
		}
	}
}

// Client is an HTTP client with its own cookie jar and optional headers.
type Client struct {
	E       *Env
	HTTP    *http.Client
	Headers map[string]string
}

func (e *Env) Client() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{E: e, HTTP: &http.Client{Jar: jar, Timeout: 60 * time.Second}, Headers: map[string]string{}}
}

// WithHeader returns a copy of c with an extra header.
func (c *Client) WithHeader(k, v string) *Client {
	h := map[string]string{}
	for a, b := range c.Headers {
		h[a] = b
	}
	h[k] = v
	return &Client{E: c.E, HTTP: c.HTTP, Headers: h}
}

// Resp is a decoded response.
type Resp struct {
	Status int
	Body   map[string]any
	Raw    []byte
	Header http.Header
}

func (r *Resp) Code() string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		s, _ := e["code"].(string)
		return s
	}
	return ""
}

func (r *Resp) Str(path ...string) string {
	v := r.Get(path...)
	s, _ := v.(string)
	return s
}

// Get walks nested maps and arrays: Get("resource", "id"), Get("rows", "0", "x").
func (r *Resp) Get(path ...string) any {
	var cur any = r.Body
	for _, p := range path {
		switch m := cur.(type) {
		case map[string]any:
			cur = m[p]
		case []any:
			var i int
			fmt.Sscanf(p, "%d", &i)
			if i >= len(m) {
				return nil
			}
			cur = m[i]
		default:
			return nil
		}
	}
	return cur
}

func (r *Resp) String() string { return fmt.Sprintf("%d %s", r.Status, string(r.Raw)) }

// Do sends a JSON request. body may be nil, []byte (sent raw) or any JSON value.
func (c *Client) Do(method, path string, body any) *Resp {
	c.E.T.Helper()
	var rd io.Reader
	raw := false
	switch b := body.(type) {
	case nil:
	case []byte:
		rd, raw = bytes.NewReader(b), true
	default:
		buf, _ := json.Marshal(b)
		rd = bytes.NewReader(buf)
	}
	url := path
	if !strings.HasPrefix(path, "http") {
		url = c.E.URL + path
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		c.E.T.Fatal(err)
	}
	if body != nil && !raw {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		c.E.T.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	out := &Resp{Status: res.StatusCode, Raw: data, Header: res.Header, Body: map[string]any{}}
	_ = json.Unmarshal(data, &out.Body)
	return out
}

// Must fails the test unless the response has the wanted status.
func (r *Resp) Must(t *testing.T, status int) *Resp {
	t.Helper()
	if r.Status != status {
		t.Fatalf("want status %d, got %s", status, r.String())
	}
	return r
}

// Admin completes first-run setup and returns a signed-in admin client.
func (e *Env) Admin() *Client {
	e.T.Helper()
	tok, err := e.API.IssueSetupToken(context.Background())
	if err != nil || tok == "" {
		e.T.Fatalf("setup token: %v", err)
	}
	c := e.Client()
	c.Do("POST", "/v1/setup", map[string]any{"token": tok, "email": "admin@example.com", "name": "Admin"}).Must(e.T, 201)
	return c
}

// Invite creates an invite with the admin client and accepts it in a new client.
func (e *Env) Invite(admin *Client, email, role string) *Client {
	e.T.Helper()
	res := admin.Do("POST", "/v1/admin/invites", map[string]any{"email": email, "role": role}).Must(e.T, 201)
	link := res.Str("link")
	token := link[strings.Index(link, "token=")+6:]
	c := e.Client()
	c.Do("POST", "/v1/auth/invites/accept", map[string]any{"token": token, "name": email}).Must(e.T, 201)
	return c
}

// Project is a created project with its keys.
type Project struct {
	ID, Ref, URL, Publishable, Secret string
}

func (e *Env) Project(c *Client, name string) *Project {
	e.T.Helper()
	res := c.Do("POST", "/v1/projects", map[string]any{"name": name}).Must(e.T, 201)
	p := &Project{ID: res.Str("project", "id"), Ref: res.Str("project", "ref"), URL: res.Str("project", "url")}
	keys := c.Do("GET", "/v1/projects/"+p.ID+"/keys", nil).Must(e.T, 200)
	p.Publishable, p.Secret = keys.Str("publishableKey"), keys.Str("secretKey")
	return p
}

// Service adds a service and waits until it is active.
func (e *Env) Service(c *Client, p *Project, kind, name string) string {
	e.T.Helper()
	res := c.Do("POST", "/v1/projects/"+p.ID+"/resources", map[string]any{"kind": kind, "name": name}).Must(e.T, 202)
	id := res.Str("resource", "id")
	e.WaitStatus(c, id, "active")
	return id
}

func (e *Env) WaitStatus(c *Client, id, status string) *Resp {
	e.T.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		res := c.Do("GET", "/v1/resources/"+id, nil)
		if res.Str("resource", "status") == status {
			return res
		}
		if time.Now().After(deadline) {
			e.T.Fatalf("service %s never became %s: %s", id, status, res.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ProjectClient returns a client that calls the project API with a key.
func (e *Env) ProjectClient(key string) *Client {
	return e.Client().WithHeader("apikey", key)
}

// Drain runs ready jobs right away.
func (e *Env) Drain() {
	if err := e.Worker.Drain(context.Background()); err != nil {
		e.T.Fatal(err)
	}
}
