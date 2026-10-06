package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

func redisAs(t *testing.T, c *testenv.Client, id string) (*redis.Client, string) {
	t.Helper()
	conn := c.Do("GET", "/v1/resources/"+id+"/connection", nil).Must(t, 200)
	opt, err := redis.ParseURL(conn.Str("native", "url"))
	if err != nil {
		t.Fatal(err)
	}
	opt.DisableIdentity = true
	rc := redis.NewClient(opt)
	t.Cleanup(func() { rc.Close() })
	return rc, conn.Str("native", "keyPrefix")
}

func TestCacheServiceAndIsolation(t *testing.T) {
	e := testenv.New(t)
	ctx := context.Background()
	admin := e.Admin()
	pa := e.Project(admin, "a")
	pb := e.Project(admin, "b")
	ida := e.Service(admin, pa, "cache", "kv")
	idb := e.Service(admin, pb, "cache", "kv")
	api := e.ProjectClient(pa.Secret)
	base := "/p/" + pa.Ref + "/cache/kv"

	api.Do("PUT", base+"/keys/user:1", map[string]any{"value": `{"name":"Ada"}`, "ttl": 60}).Must(t, 200)
	got := api.Do("GET", base+"/keys/user:1", nil).Must(t, 200)
	if got.Str("value") != `{"name":"Ada"}` || got.Get("ttl") == nil {
		t.Fatalf("get: %s", got)
	}
	if r := api.Do("GET", base+"/keys/missing", nil); r.Status != 404 {
		t.Fatalf("missing key: %s", r)
	}
	if v := api.Do("POST", base+"/incr/hits", map[string]any{"by": 5}).Must(t, 200).Get("value").(float64); v != 5 {
		t.Fatalf("incr: %v", v)
	}
	batch := api.Do("POST", base+"/batch", map[string]any{"ops": []any{
		map[string]any{"op": "set", "key": "a", "value": "1"},
		map[string]any{"op": "get", "key": "a"},
		map[string]any{"op": "del", "key": "a"},
		map[string]any{"op": "get", "key": "a"},
	}}).Must(t, 200)
	if batch.Str("results", "1", "value") != "1" || batch.Get("results", "3", "value") != nil {
		t.Fatalf("batch: %s", batch)
	}
	keys := api.Do("GET", base+"/keys?prefix=user", nil).Must(t, 200)
	if len(keys.Get("keys").([]any)) != 1 || keys.Str("keys", "0") != "user:1" {
		t.Fatalf("list: %s", keys)
	}

	// Native access with the service's own ACL user.
	ra, prefixA := redisAs(t, admin, ida)
	rb, prefixB := redisAs(t, admin, idb)
	if v, err := ra.Get(ctx, prefixA+"user:1").Result(); err != nil || v != `{"name":"Ada"}` {
		t.Fatalf("native read: %q %v", v, err)
	}
	if err := rb.Set(ctx, prefixB+"secret", "b-only", 0).Err(); err != nil {
		t.Fatal(err)
	}

	t.Run("keys outside the prefix are denied", func(t *testing.T) {
		if err := ra.Get(ctx, prefixB+"secret").Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
			t.Fatalf("A read B's key: %v", err)
		}
		if err := ra.Set(ctx, "global", "x", 0).Err(); err == nil {
			t.Fatal("A wrote outside its prefix")
		}
	})
	t.Run("dangerous commands are denied", func(t *testing.T) {
		for _, cmd := range [][]any{{"keys", "*"}, {"scan", "0"}, {"flushall"}, {"flushdb"}, {"config", "get", "*"},
			{"eval", "return 1", "0"}, {"script", "flush"}, {"function", "list"}, {"acl", "list"}, {"client", "list"},
			{"info"}, {"debug", "sleep", "0"}, {"monitor"}, {"select", "1"}, {"randomkey"}} {
			if err := ra.Do(ctx, cmd...).Err(); err == nil || !(strings.Contains(err.Error(), "NOPERM") || strings.Contains(err.Error(), "not allowed")) {
				t.Errorf("%v allowed: %v", cmd, err)
			}
		}
	})
	t.Run("API keys cannot escape the prefix", func(t *testing.T) {
		// B's full key name is just a long key inside A's namespace.
		api.Do("PUT", base+"/keys/"+prefixB+"secret", map[string]any{"value": "x"}).Must(t, 200)
		if v, _ := rb.Get(ctx, prefixB+"secret").Result(); v != "b-only" {
			t.Fatalf("A overwrote B's key: %q", v)
		}
		if r := e.ProjectClient(pa.Secret).Do("GET", "/p/"+pb.Ref+"/cache/kv/keys/secret", nil); r.Status != 401 {
			t.Fatalf("A's key on B's project: %s", r)
		}
	})
	t.Run("memory limit locks writes", func(t *testing.T) {
		admin.Do("POST", "/v1/admin/plans", map[string]any{"id": "cache-tiny", "kind": "cache", "name": "Tiny", "limits": map[string]any{"memory_mb": 1, "connections": 2}}).Must(t, 201)
		admin.Do("PATCH", "/v1/resources/"+ida, map[string]any{"plan": "cache-tiny"}).Must(t, 200)
		big := strings.Repeat("x", 300_000)
		for i := 0; i < 5; i++ {
			api.Do("PUT", fmt.Sprintf("%s/keys/big%d", base, i), map[string]any{"value": big}).Must(t, 200)
		}
		if err := e.App.SampleUsage(ctx); err != nil {
			t.Fatal(err)
		}
		r := api.Do("PUT", base+"/keys/more", map[string]any{"value": "x"})
		if r.Code() != "quota_exceeded" {
			t.Fatalf("write over limit: %s", r)
		}
		api.Do("GET", base+"/keys/big0", nil).Must(t, 200)
		for i := 0; i < 5; i++ {
			api.Do("DELETE", fmt.Sprintf("%s/keys/big%d", base, i), nil).Must(t, 200)
		}
		if err := e.App.SampleUsage(ctx); err != nil {
			t.Fatal(err)
		}
		api.Do("PUT", base+"/keys/more", map[string]any{"value": "x"}).Must(t, 200)
	})
	t.Run("pause disables the ACL user", func(t *testing.T) {
		admin.Do("POST", "/v1/resources/"+idb+"/pause", nil).Must(t, 200)
		fresh, _ := redisAs(t, admin, idb)
		if err := fresh.Ping(ctx).Err(); err == nil {
			t.Fatal("paused cache accepted a login")
		}
		admin.Do("POST", "/v1/resources/"+idb+"/resume", nil).Must(t, 200)
		fresh2, _ := redisAs(t, admin, idb)
		if err := fresh2.Ping(ctx).Err(); err != nil {
			t.Fatalf("resumed cache: %v", err)
		}
	})
}

func TestStorageServiceAndIsolation(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	pa := e.Project(admin, "a")
	pb := e.Project(admin, "b")
	e.Service(admin, pa, "storage", "files")
	e.Service(admin, pb, "storage", "files")
	api := e.ProjectClient(pa.Secret)
	apiB := e.ProjectClient(pb.Secret)
	base := "/p/" + pa.Ref + "/storage/files"

	api.Do("PUT", base+"/objects/avatars/1.txt", []byte("hello")).Must(t, 201)
	apiB.Do("PUT", "/p/"+pb.Ref+"/storage/files/objects/secret.txt", []byte("b-only")).Must(t, 201)
	get := api.Do("GET", base+"/objects/avatars/1.txt", nil).Must(t, 200)
	if string(get.Raw) != "hello" || !strings.HasPrefix(get.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("download: %s %v", get.Raw, get.Header)
	}
	list := api.Do("GET", base+"/objects?prefix=avatars/", nil).Must(t, 200)
	if list.Str("objects", "0", "key") != "avatars/1.txt" || len(list.Get("objects").([]any)) != 1 {
		t.Fatalf("list: %s", list)
	}

	t.Run("path traversal is rejected", func(t *testing.T) {
		for _, key := range []string{"..%2F..%2Fetc%2Fpasswd", "a/../../x", "%2Fetc%2Fpasswd", ".sbtmp-x"} {
			r := api.Do("PUT", base+"/objects/"+key, []byte("x"))
			if r.Status == 201 {
				t.Errorf("key %q accepted", key)
			}
		}
		if r := api.Do("GET", base+"/objects/..%2F"+"sb-x%2Fsecret.txt", nil); r.Status == 200 {
			t.Fatalf("traversal read: %s", r)
		}
	})
	t.Run("other tenants' objects are unreachable", func(t *testing.T) {
		if r := api.Do("GET", "/p/"+pb.Ref+"/storage/files/objects/secret.txt", nil); r.Status != 401 {
			t.Fatalf("A's key on B: %s", r)
		}
		if r := api.Do("GET", base+"/objects/secret.txt", nil); r.Status != 404 {
			t.Fatalf("B's object in A's bucket: %s", r)
		}
	})
	t.Run("signed URLs", func(t *testing.T) {
		url := api.Do("POST", base+"/sign", map[string]any{"key": "avatars/1.txt", "expiresIn": 60}).Must(t, 200).Str("url")
		anon := e.Client()
		if r := anon.Do("GET", url, nil); r.Status != 200 || string(r.Raw) != "hello" {
			t.Fatalf("signed get: %s", r)
		}
		// Edited to point at another key, or to another project.
		if r := anon.Do("GET", strings.Replace(url, "avatars/1.txt", "secret.txt", 1), nil); r.Status != 401 {
			t.Fatalf("edited key: %s", r)
		}
		if r := anon.Do("GET", strings.Replace(url, pa.Ref, pb.Ref, 1), nil); r.Status != 401 {
			t.Fatalf("edited project: %s", r)
		}
		if r := anon.Do("PUT", url, []byte("x")); r.Status == 201 {
			t.Fatal("GET URL used for PUT")
		}
		put := api.Do("POST", base+"/sign", map[string]any{"key": "uploads/new.bin", "method": "PUT", "expiresIn": 60}).Must(t, 200).Str("url")
		anon.Do("PUT", put, []byte("uploaded")).Must(t, 201)
		if string(api.Do("GET", base+"/objects/uploads/new.bin", nil).Must(t, 200).Raw) != "uploaded" {
			t.Fatal("signed upload content")
		}
		expired := api.Do("POST", base+"/sign", map[string]any{"key": "avatars/1.txt", "expiresIn": 1}).Must(t, 200).Str("url")
		time.Sleep(2100 * time.Millisecond)
		if r := anon.Do("GET", expired, nil); r.Status != 401 {
			t.Fatalf("expired URL: %s", r)
		}
	})
	t.Run("quota", func(t *testing.T) {
		admin.Do("POST", "/v1/admin/plans", map[string]any{"id": "storage-tiny", "kind": "storage", "name": "Tiny", "limits": map[string]any{"storage_mb": 1, "max_upload_mb": 1}}).Must(t, 201)
		res := admin.Do("GET", "/v1/projects/"+pa.ID+"/resources", nil).Must(t, 200)
		id := res.Str("resources", "0", "id")
		admin.Do("PATCH", "/v1/resources/"+id, map[string]any{"plan": "storage-tiny"}).Must(t, 200)
		big := make([]byte, 700_000)
		api.Do("PUT", base+"/objects/big1", big).Must(t, 201)
		r := api.Do("PUT", base+"/objects/big2", big)
		if r.Status != 413 && r.Code() != "quota_exceeded" {
			t.Fatalf("write past quota: %s", r)
		}
	})
	t.Run("delete", func(t *testing.T) {
		if !api.Do("DELETE", base+"/objects/avatars/1.txt", nil).Must(t, 200).Get("deleted").(bool) {
			t.Fatal("delete")
		}
		if r := api.Do("GET", base+"/objects/avatars/1.txt", nil); r.Status != 404 {
			t.Fatalf("deleted object: %s", r)
		}
	})
}

func TestVectorService(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	p := e.Project(admin, "v")
	e.Service(admin, p, "vector", "docs")
	api := e.ProjectClient(p.Secret)
	base := "/p/" + p.Ref + "/vector/docs/collections"

	api.Do("POST", base, map[string]any{"name": "pages", "dimension": 3}).Must(t, 201)
	if r := api.Do("POST", base, map[string]any{"name": "pages", "dimension": 3}); r.Status != 409 {
		t.Fatalf("duplicate collection: %s", r)
	}
	api.Do("POST", base+"/pages/upsert", map[string]any{"items": []any{
		map[string]any{"id": "x", "vector": []float64{1, 0, 0}, "metadata": map[string]any{"lang": "en"}},
		map[string]any{"id": "y", "vector": []float64{0, 1, 0}, "metadata": map[string]any{"lang": "fr"}},
		map[string]any{"id": "z", "vector": []float64{0.9, 0.1, 0}, "metadata": map[string]any{"lang": "en"}},
	}}).Must(t, 200)
	if r := api.Do("POST", base+"/pages/upsert", map[string]any{"items": []any{map[string]any{"id": "bad", "vector": []float64{1, 2}}}}); r.Status != 422 {
		t.Fatalf("wrong dimension: %s", r)
	}
	q := api.Do("POST", base+"/pages/query", map[string]any{"vector": []float64{1, 0, 0}, "topK": 2}).Must(t, 200)
	if q.Str("matches", "0", "id") != "x" || q.Str("matches", "1", "id") != "z" {
		t.Fatalf("query order: %s", q)
	}
	if s := q.Get("matches", "0", "score").(float64); s < 0.99 {
		t.Fatalf("cosine score: %v", s)
	}
	f := api.Do("POST", base+"/pages/query", map[string]any{"vector": []float64{1, 0, 0}, "filter": map[string]any{"lang": "fr"}}).Must(t, 200)
	if len(f.Get("matches").([]any)) != 1 || f.Str("matches", "0", "id") != "y" {
		t.Fatalf("filter: %s", f)
	}
	item := api.Do("GET", base+"/pages/items/z", nil).Must(t, 200)
	if len(item.Get("vector").([]any)) != 3 {
		t.Fatalf("item: %s", item)
	}
	api.Do("POST", base+"/pages/delete", map[string]any{"ids": []string{"x"}}).Must(t, 200)
	list := api.Do("GET", base, nil).Must(t, 200)
	if list.Get("collections", "0", "count").(float64) != 2 {
		t.Fatalf("count after delete: %s", list)
	}
	api.Do("DELETE", base+"/pages", nil).Must(t, 204)
	if r := api.Do("POST", base+"/pages/query", map[string]any{"vector": []float64{1, 0, 0}}); r.Status != 404 {
		t.Fatalf("dropped collection: %s", r)
	}
}

func TestQueueService(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	p := e.Project(admin, "q")
	e.Service(admin, p, "queue", "jobs")
	api := e.ProjectClient(p.Secret)
	base := "/p/" + p.Ref + "/queue/jobs/queues"

	api.Do("POST", base, map[string]any{"name": "emails", "visibilityTimeout": 1, "maxReceives": 2}).Must(t, 201)
	api.Do("POST", base+"/emails/send", map[string]any{"messages": []any{
		map[string]any{"body": map[string]any{"to": "a@example.com"}},
		map[string]any{"body": "plain", "delay": 60},
	}}).Must(t, 201)

	got := api.Do("POST", base+"/emails/receive", map[string]any{"max": 10}).Must(t, 200)
	msgs := got.Get("messages").([]any)
	if len(msgs) != 1 || got.Str("messages", "0", "body", "to") != "a@example.com" {
		t.Fatalf("receive (the delayed one must stay hidden): %s", got)
	}
	// In flight: nothing else to take.
	if n := len(api.Do("POST", base+"/emails/receive", map[string]any{"max": 10}).Must(t, 200).Get("messages").([]any)); n != 0 {
		t.Fatalf("in-flight message delivered twice")
	}
	// After the visibility timeout it comes back, then dies after maxReceives.
	time.Sleep(1100 * time.Millisecond)
	again := api.Do("POST", base+"/emails/receive", map[string]any{}).Must(t, 200)
	if again.Get("messages", "0", "receives").(float64) != 2 {
		t.Fatalf("redelivery: %s", again)
	}
	time.Sleep(1100 * time.Millisecond)
	api.Do("POST", base+"/emails/receive", map[string]any{}).Must(t, 200)
	stats := api.Do("GET", base+"/emails/stats", nil).Must(t, 200)
	if stats.Get("dead").(float64) != 1 {
		t.Fatalf("dead letters: %s", stats)
	}
	api.Do("POST", base+"/emails/redrive", nil).Must(t, 200)
	m := api.Do("POST", base+"/emails/receive", map[string]any{}).Must(t, 200)
	id := m.Get("messages", "0", "id")
	api.Do("POST", base+"/emails/ack", map[string]any{"ids": []any{id}}).Must(t, 200)
	stats = api.Do("GET", base+"/emails/stats", nil).Must(t, 200)
	if stats.Get("ready").(float64) != 0 || stats.Get("dead").(float64) != 0 || stats.Get("inFlight").(float64) != 1 {
		t.Fatalf("after ack: %s", stats)
	}
	if r := api.Do("POST", base+"/nope/send", map[string]any{"messages": []any{map[string]any{"body": 1}}}); r.Status != 404 {
		t.Fatalf("unknown queue: %s", r)
	}
}

func TestAuthService(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	p := e.Project(admin, "auth")
	authID := e.Service(admin, p, "auth", "auth")
	if r := admin.Do("POST", "/v1/projects/"+p.ID+"/resources", map[string]any{"kind": "auth", "name": "auth2"}); r.Status != 409 {
		t.Fatalf("second auth service: %s", r)
	}
	pub := e.ProjectClient(p.Publishable)
	sec := e.ProjectClient(p.Secret)
	base := "/p/" + p.Ref + "/auth"

	// Email and password.
	su := pub.Do("POST", base+"/signup", map[string]any{"email": "user@example.com", "password": pw, "data": map[string]any{"plan": "free"}}).Must(t, 201)
	access := su.Str("session", "access_token")
	refresh := su.Str("session", "refresh_token")
	if access == "" || refresh == "" {
		t.Fatalf("signup session: %s", su)
	}
	if r := pub.Do("POST", base+"/signup", map[string]any{"email": "user@example.com", "password": pw}); r.Status != 409 {
		t.Fatalf("duplicate signup: %s", r)
	}
	if r := pub.Do("POST", base+"/token?grant_type=password", map[string]any{"email": "user@example.com", "password": "wrong password"}); r.Status != 401 {
		t.Fatalf("wrong password: %s", r)
	}
	login := pub.Do("POST", base+"/token?grant_type=password", map[string]any{"email": "user@example.com", "password": pw}).Must(t, 200)
	access = login.Str("session", "access_token")

	userClient := pub.WithHeader("Authorization", "Bearer "+access)
	me := userClient.Do("GET", base+"/user", nil).Must(t, 200)
	if me.Str("user", "email") != "user@example.com" || me.Str("user", "data", "plan") != "free" {
		t.Fatalf("user: %s", me)
	}
	if r := pub.WithHeader("Authorization", "Bearer "+access+"x").Do("GET", base+"/user", nil); r.Status != 401 {
		t.Fatalf("tampered token: %s", r)
	}
	// The JWT secret is shown to the owner so servers can verify tokens.
	conn := admin.Do("GET", "/v1/resources/"+authID+"/connection", nil).Must(t, 200)
	if conn.Str("jwtSecret") == "" {
		t.Fatalf("no jwt secret: %s", conn)
	}

	// Refresh rotates; reusing an old refresh token ends the family.
	r1 := pub.Do("POST", base+"/token?grant_type=refresh_token", map[string]any{"refresh_token": refresh}).Must(t, 200)
	if r := pub.Do("POST", base+"/token?grant_type=refresh_token", map[string]any{"refresh_token": refresh}); r.Status != 401 {
		t.Fatalf("refresh token reused: %s", r)
	}
	if r := pub.Do("POST", base+"/token?grant_type=refresh_token", map[string]any{"refresh_token": r1.Str("session", "refresh_token")}); r.Status != 401 {
		t.Fatalf("family not revoked after reuse: %s", r)
	}

	// Password change through the user endpoint.
	userClient.Do("PUT", base+"/user", map[string]any{"password": "a newer password"}).Must(t, 200)
	pub.Do("POST", base+"/token?grant_type=password", map[string]any{"email": "user@example.com", "password": "a newer password"}).Must(t, 200)

	// Recovery: code by email, then a session.
	pub.Do("POST", base+"/recover", map[string]any{"email": "user@example.com"}).Must(t, 200)
	msg, ok := e.Mail.Last("user@example.com")
	if !ok {
		t.Fatal("no recovery email")
	}
	code := msg.Subject[len(msg.Subject)-6:]
	if r := pub.Do("POST", base+"/verify", map[string]any{"email": "user@example.com", "code": "000000"}); r.Status != 401 && code != "000000" {
		t.Fatalf("wrong code accepted: %s", r)
	}
	pub.Do("POST", base+"/verify", map[string]any{"email": "user@example.com", "code": code}).Must(t, 200)
	if r := pub.Do("POST", base+"/verify", map[string]any{"email": "user@example.com", "code": code}); r.Status != 401 {
		t.Fatalf("code reused: %s", r)
	}

	// Passwordless sign-in with the generated link token.
	sec.Do("PATCH", base+"/admin/settings", map[string]any{"siteUrl": "https://app.example.com"}).Must(t, 200)
	if r := pub.Do("POST", base+"/otp", map[string]any{"email": "x@example.com", "redirectTo": "https://evil.example.net/cb"}); r.Status != 422 {
		t.Fatalf("unlisted redirect: %s", r)
	}
	gen := sec.Do("POST", base+"/admin/generate-link", map[string]any{"email": "new@example.com"}).Must(t, 200)
	if !strings.HasPrefix(gen.Str("link"), "https://app.example.com?token=") {
		t.Fatalf("link: %s", gen)
	}
	v := pub.Do("POST", base+"/verify", map[string]any{"token": gen.Str("token")}).Must(t, 200)
	if v.Str("user", "email") != "new@example.com" {
		t.Fatalf("otp sign-in: %s", v)
	}

	// Admin: list, ban, delete. Publishable keys cannot use admin routes.
	if r := pub.Do("GET", base+"/admin/users", nil); r.Status != 403 {
		t.Fatalf("publishable on admin: %s", r)
	}
	users := sec.Do("GET", base+"/admin/users", nil).Must(t, 200)
	if users.Get("total").(float64) != 2 {
		t.Fatalf("users: %s", users)
	}
	uid := me.Str("user", "id")
	sec.Do("PATCH", base+"/admin/users/"+uid, map[string]any{"banned": true}).Must(t, 200)
	if r := pub.Do("POST", base+"/token?grant_type=password", map[string]any{"email": "user@example.com", "password": "a newer password"}); r.Status != 403 {
		t.Fatalf("banned sign-in: %s", r)
	}
	sec.Do("DELETE", base+"/admin/users/"+uid, nil).Must(t, 204)

	// Sign-ups can be closed.
	sec.Do("PATCH", base+"/admin/settings", map[string]any{"allowSignups": false}).Must(t, 200)
	if r := pub.Do("POST", base+"/signup", map[string]any{"email": "late@example.com", "password": pw}); r.Status != 403 {
		t.Fatalf("signup when closed: %s", r)
	}

	// The token of one project is not valid in another.
	p2 := e.Project(admin, "auth2")
	e.Service(admin, p2, "auth", "auth")
	other := e.ProjectClient(p2.Publishable).WithHeader("Authorization", "Bearer "+v.Str("session", "access_token"))
	if r := other.Do("GET", "/p/"+p2.Ref+"/auth/user", nil); r.Status != 401 {
		t.Fatalf("token accepted by another project: %s", r)
	}
	_ = http.StatusOK
}
