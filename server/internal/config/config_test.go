package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func valid() map[string]string {
	return map[string]string{
		"SPARKBASE_DOMAIN":     "example.com",
		"DATABASE_URL":         "postgres://x@localhost/sb",
		"REDIS_URL":            "redis://localhost:6379",
		"ENCRYPTION_KEY":       "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"TRAEFIK_CONFIG_TOKEN": "t",
	}
}

func TestLoadValid(t *testing.T) {
	c, err := Load(env(valid()))
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicAPIURL != "https://api.example.com" || c.ProjectURL("abc") != "https://api.example.com/p/abc" {
		t.Fatalf("unexpected url %q", c.PublicAPIURL)
	}
	if c.Port != 8787 || c.NativeMode != "traefik" || !c.SecureCookies {
		t.Fatalf("bad defaults %+v", c)
	}
}

func TestLoadMissingListsEveryVariable(t *testing.T) {
	_, err := Load(env(map[string]string{}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, name := range []string{"SPARKBASE_DOMAIN", "DATABASE_URL", "REDIS_URL", "ENCRYPTION_KEY", "TRAEFIK_CONFIG_TOKEN"} {
		if !strings.Contains(err.Error(), name+" is required") {
			t.Errorf("error does not mention %s: %v", name, err)
		}
	}
}

func TestLoadBadKey(t *testing.T) {
	m := valid()
	m["ENCRYPTION_KEY"] = "short"
	if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("expected key error, got %v", err)
	}
}

func TestLocalhostUsesHTTP(t *testing.T) {
	m := valid()
	m["SPARKBASE_DOMAIN"] = "localhost"
	m["PUBLIC_API_URL"] = "http://localhost:8787/"
	c, err := Load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicAPIURL != "http://localhost:8787" || c.SecureCookies {
		t.Fatalf("got %q secure=%v", c.PublicAPIURL, c.SecureCookies)
	}
}
