package api_test

import (
	"strings"
	"testing"

	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

const pw = testenv.Password

func linkToken(link string) string {
	i := strings.Index(link, "token=")
	if i < 0 {
		return ""
	}
	return link[i+6:]
}

func TestFirstSignupIsAdminThenClosed(t *testing.T) {
	e := testenv.New(t)
	c := e.Client()
	if !c.Do("GET", "/v1/setup", nil).Must(t, 200).Get("open").(bool) {
		t.Fatal("setup should be open")
	}
	if r := c.Do("POST", "/v1/setup", map[string]any{"email": "a@example.com", "password": "short"}); r.Status != 422 {
		t.Fatalf("weak password: %s", r)
	}
	c.Do("POST", "/v1/setup", map[string]any{"email": "a@example.com", "password": pw}).Must(t, 201)
	if c.Do("GET", "/v1/me", nil).Must(t, 200).Str("user", "role") != "admin" {
		t.Fatal("first user must be admin")
	}
	if e.Client().Do("GET", "/v1/setup", nil).Must(t, 200).Get("open").(bool) {
		t.Fatal("setup still open after the first account")
	}
	r := e.Client().Do("POST", "/v1/setup", map[string]any{"email": "b@example.com", "password": pw})
	if r.Status != 409 || r.Code() != "setup_closed" {
		t.Fatalf("second signup: %s", r)
	}
	if r := e.Client().Do("POST", "/v1/auth/signup", map[string]any{"email": "b@example.com", "password": pw}); r.Status != 404 {
		t.Fatalf("open signup route still exists: %s", r)
	}
	_, login := e.Login("a@example.com", pw)
	login.Must(t, 200)
}

func TestLoginLogoutAndLockout(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	e.Invite(admin, "ada@example.com", "member")

	c, r := e.Login("Ada@Example.com", pw)
	r.Must(t, 200)
	if strings.Contains(string(r.Raw), "password") {
		t.Fatalf("response leaks password hash: %s", r)
	}
	if c.Do("GET", "/v1/me", nil).Must(t, 200).Str("user", "email") != "ada@example.com" {
		t.Fatal("wrong user")
	}
	c.Do("POST", "/v1/auth/logout", nil).Must(t, 204)
	if r := c.Do("GET", "/v1/me", nil); r.Status != 401 {
		t.Fatalf("after logout: %s", r)
	}

	// Same error for a wrong password and an unknown email.
	_, wrong := e.Login("ada@example.com", "not the password")
	_, unknown := e.Login("nobody@example.com", "not the password")
	if wrong.Status != 401 || string(wrong.Raw) != string(unknown.Raw) {
		t.Fatalf("responses differ: %s vs %s", wrong, unknown)
	}
	// Lockout after repeated failures, even with the right password.
	for i := 0; i < 10; i++ {
		e.Login("ada@example.com", "bad password!")
	}
	if _, r := e.Login("ada@example.com", pw); r.Code() != "account_locked" {
		t.Fatalf("not locked: %s", r)
	}
}

func TestPasswordResetAndChange(t *testing.T) {
	e := testenv.New(t)
	admin := e.Admin()
	member := e.Invite(admin, "bo@example.com", "member")

	// Reset by email: same answer for unknown addresses.
	known := e.Client().Do("POST", "/v1/auth/password/reset", map[string]any{"email": "bo@example.com"}).Must(t, 200)
	unknown := e.Client().Do("POST", "/v1/auth/password/reset", map[string]any{"email": "nobody@example.com"}).Must(t, 200)
	if string(known.Raw) != string(unknown.Raw) {
		t.Fatalf("reset responses differ")
	}
	msg, ok := e.Mail.Last("bo@example.com")
	if !ok || !strings.Contains(msg.Link, "/reset-password?token=") {
		t.Fatalf("no reset email: %+v", msg)
	}
	c := e.Client()
	c.Do("POST", "/v1/auth/password/reset/confirm", map[string]any{"token": linkToken(msg.Link), "password": "brand new password"}).Must(t, 200)
	if r := member.Do("GET", "/v1/me", nil); r.Status != 401 {
		t.Fatalf("old session survived a reset: %s", r)
	}
	if r := e.Client().Do("POST", "/v1/auth/password/reset/confirm", map[string]any{"token": linkToken(msg.Link), "password": "another password"}); r.Status != 401 {
		t.Fatalf("reset link reused: %s", r)
	}
	if _, r := e.Login("bo@example.com", pw); r.Status != 401 {
		t.Fatal("old password still works")
	}
	_, r := e.Login("bo@example.com", "brand new password")
	r.Must(t, 200)

	// Admin-generated link for installs without SMTP.
	users := admin.Do("GET", "/v1/admin/users", nil).Must(t, 200)
	var id string
	for _, u := range users.Get("users").([]any) {
		if u.(map[string]any)["email"] == "bo@example.com" {
			id = u.(map[string]any)["id"].(string)
		}
	}
	link := admin.Do("POST", "/v1/admin/users/"+id+"/reset-link", nil).Must(t, 201).Str("link")
	e.Client().Do("POST", "/v1/auth/password/reset/confirm", map[string]any{"token": linkToken(link), "password": "admin given pass"}).Must(t, 200)

	// Change password needs the current one.
	c, _ = e.Login("bo@example.com", "admin given pass")
	if r := c.Do("PATCH", "/v1/me", map[string]any{"currentPassword": "nope", "newPassword": "something else"}); r.Status != 403 {
		t.Fatalf("changed without current password: %s", r)
	}
	c.Do("PATCH", "/v1/me", map[string]any{"currentPassword": "admin given pass", "newPassword": "something else"}).Must(t, 200)
	_, r = e.Login("bo@example.com", "something else")
	r.Must(t, 200)
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
	if _, r := e.Login("bob@example.com", pw); r.Status != 401 {
		t.Fatalf("disabled user signed in: %s", r)
	}
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

	res := e.Client().Do("POST", "/v1/setup", map[string]any{"email": "x@example.com", "password": pw})
	if res.Header.Get("X-RateLimit-Limit") == "" {
		t.Fatalf("missing rate limit headers: %v", res.Header)
	}
	var last *testenv.Resp
	for i := 0; i < 12; i++ {
		last = e.Client().Do("POST", "/v1/setup", map[string]any{"email": "x@example.com", "password": pw})
	}
	if last.Status != 429 || last.Header.Get("Retry-After") == "" {
		t.Fatalf("setup not rate limited: %s", last)
	}
}
