package api_test

import (
	"context"
	"strings"
	"testing"

	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

func linkToken(link string) string {
	i := strings.Index(link, "token=")
	if i < 0 {
		return ""
	}
	return link[i+6:]
}

func TestSetupOnce(t *testing.T) {
	e := testenv.New(t)
	c := e.Client()
	if !c.Do("GET", "/v1/setup", nil).Must(t, 200).Get("open").(bool) {
		t.Fatal("setup should be open")
	}
	tok, err := e.API.IssueSetupToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := c.Do("POST", "/v1/setup", map[string]any{"token": "wrong", "email": "a@example.com"}); r.Status != 401 {
		t.Fatalf("wrong token: %s", r)
	}
	c.Do("POST", "/v1/setup", map[string]any{"token": tok, "email": "a@example.com"}).Must(t, 201)
	if c.Do("GET", "/v1/me", nil).Must(t, 200).Str("user", "role") != "admin" {
		t.Fatal("first user must be admin")
	}
	r := e.Client().Do("POST", "/v1/setup", map[string]any{"token": tok, "email": "b@example.com"})
	if r.Status != 409 || r.Code() != "setup_closed" {
		t.Fatalf("second setup: %s", r)
	}
	if tok2, _ := e.API.IssueSetupToken(context.Background()); tok2 != "" {
		t.Fatal("setup token issued after setup")
	}
}

func TestMagicLinkSignIn(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	member := e.Invite(admin, "ada@example.com", "member")
	member.Do("GET", "/v1/me", nil).Must(t, 200)

	// Sign in again from a fresh browser with a magic link.
	c := e.Client()
	c.Do("POST", "/v1/auth/magic-link", map[string]any{"email": "Ada@Example.com"}).Must(t, 200)
	msg, ok := e.Mail.Last("ada@example.com")
	if !ok {
		t.Fatal("no email sent")
	}
	tok := linkToken(msg.Link)
	c.Do("POST", "/v1/auth/magic-link/verify", map[string]any{"token": tok}).Must(t, 200)
	if c.Do("GET", "/v1/me", nil).Must(t, 200).Str("user", "email") != "ada@example.com" {
		t.Fatal("wrong user")
	}
	// Links work once.
	if r := e.Client().Do("POST", "/v1/auth/magic-link/verify", map[string]any{"token": tok}); r.Status != 401 {
		t.Fatalf("reused link: %s", r)
	}
	// Logout ends the session.
	c.Do("POST", "/v1/auth/logout", nil).Must(t, 204)
	if r := c.Do("GET", "/v1/me", nil); r.Status != 401 {
		t.Fatalf("after logout: %s", r)
	}
}

func TestMagicLinkSameResponseForUnknownEmail(t *testing.T) {
	e := testenv.New(t)
	e.Admin()
	c := e.Client()
	known := c.Do("POST", "/v1/auth/magic-link", map[string]any{"email": "admin@example.com"}).Must(t, 200)
	unknown := c.Do("POST", "/v1/auth/magic-link", map[string]any{"email": "nobody@example.com"}).Must(t, 200)
	if string(known.Raw) != string(unknown.Raw) {
		t.Fatalf("responses differ: %s vs %s", known.Raw, unknown.Raw)
	}
	if e.Mail.Count("nobody@example.com") != 0 {
		t.Fatal("invite-only install emailed an unknown address")
	}
}

func TestOpenSignup(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	admin.Do("PATCH", "/v1/admin/settings", map[string]any{"signupMode": "open"}).Must(t, 200)
	c := e.Client()
	c.Do("POST", "/v1/auth/magic-link", map[string]any{"email": "new@example.com"}).Must(t, 200)
	msg, ok := e.Mail.Last("new@example.com")
	if !ok {
		t.Fatal("no sign-up email")
	}
	r := c.Do("POST", "/v1/auth/magic-link/verify", map[string]any{"token": linkToken(msg.Link)}).Must(t, 200)
	if r.Str("user", "role") != "member" {
		t.Fatalf("open sign-up role: %s", r)
	}
}

func TestDisabledUserAndAdminRoutes(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	member := e.Invite(admin, "bob@example.com", "member")
	if r := member.Do("GET", "/v1/admin/users", nil); r.Status != 403 {
		t.Fatalf("member on admin route: %s", r)
	}
	users := admin.Do("GET", "/v1/admin/users", nil).Must(t, 200)
	var bobID string
	for _, u := range users.Get("users").([]any) {
		m := u.(map[string]any)
		if m["email"] == "bob@example.com" {
			bobID = m["id"].(string)
		}
	}
	admin.Do("PATCH", "/v1/admin/users/"+bobID, map[string]any{"disabled": true}).Must(t, 200)
	if r := member.Do("GET", "/v1/me", nil); r.Status != 401 {
		t.Fatalf("disabled user still signed in: %s", r)
	}
	// Disabled users get no link.
	e.Client().Do("POST", "/v1/auth/magic-link", map[string]any{"email": "bob@example.com"}).Must(t, 200)
	if e.Mail.Count("bob@example.com") != 1 { // only the invite
		t.Fatal("disabled user was sent a sign-in link")
	}
	// The last admin cannot be demoted.
	me := admin.Do("GET", "/v1/me", nil).Must(t, 200)
	if r := admin.Do("PATCH", "/v1/admin/users/"+me.Str("user", "id"), map[string]any{"role": "member"}); r.Status != 409 {
		t.Fatalf("demoted last admin: %s", r)
	}
}

func TestAccountKeysAndRateLimit(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	p := e.Project(admin, "keys")
	other := e.Project(admin, "other")

	full := admin.Do("POST", "/v1/api-keys", map[string]any{"name": "ci"}).Must(t, 201)
	key := e.Client().WithHeader("Authorization", "Bearer "+full.Str("secret"))
	key.Do("GET", "/v1/projects", nil).Must(t, 200)

	scoped := admin.Do("POST", "/v1/api-keys", map[string]any{"name": "scoped", "projectId": p.ID}).Must(t, 201)
	sk := e.Client().WithHeader("Authorization", "Bearer "+scoped.Str("secret"))
	sk.Do("GET", "/v1/projects/"+p.ID, nil).Must(t, 200)
	if r := sk.Do("GET", "/v1/projects/"+other.ID, nil); r.Status != 404 {
		t.Fatalf("scoped key reached another project: %s", r)
	}
	if r := sk.Do("GET", "/v1/admin/users", nil); r.Status != 403 {
		t.Fatalf("scoped key on admin route: %s", r)
	}

	admin.Do("DELETE", "/v1/api-keys/"+full.Str("key", "id"), nil).Must(t, 204)
	if r := key.Do("GET", "/v1/projects", nil); r.Status != 401 {
		t.Fatalf("revoked key works: %s", r)
	}
	if r := e.Client().WithHeader("Authorization", "Bearer sb_notarealkey").Do("GET", "/v1/me", nil); r.Status != 401 {
		t.Fatalf("bogus key: %s", r)
	}

	res := e.Client().Do("POST", "/v1/setup", map[string]any{"token": "x", "email": "x@example.com"})
	if res.Header.Get("X-RateLimit-Limit") == "" {
		t.Fatalf("missing rate limit headers: %v", res.Header)
	}
	var last *testenv.Resp
	for i := 0; i < 12; i++ {
		last = e.Client().Do("POST", "/v1/setup", map[string]any{"token": "x", "email": "x@example.com"})
	}
	if last.Status != 429 || last.Header.Get("Retry-After") == "" {
		t.Fatalf("setup not rate limited: %s", last)
	}
}
