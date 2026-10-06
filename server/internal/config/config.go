// Package config reads and validates environment variables at startup.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Config struct {
	Domain             string
	PublicAPIURL       string // e.g. https://api.example.com; project URLs are built from it
	DashboardURL       string // used in invite and reset links
	DatabaseURL        string
	RedisURL           string
	RedisPrefix        string
	EncryptionKey      []byte
	TraefikConfigToken string
	Port               int
	NativeMode         string // "traefik" or "direct"
	TLSMode            string
	ACMEEmail          string
	SMTPURL            string
	MailFrom           string
	BootstrapPGHost    string
	BootstrapCacheHost string
	BootstrapStorage   string
	BackupDir          string
	Mode               string // "all", "api" or "worker"
	// Public ports of Traefik's TCP entrypoints, used in native connection strings.
	NativePGPort       int
	NativePGPooledPort int
	NativeRedisPort    int
	SecureCookies      bool
	LogLevel           string
}

// Load builds a Config from getenv and returns one error that lists every problem.
func Load(getenv func(string) string) (*Config, error) {
	var problems []string
	need := func(name string) string {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			problems = append(problems, fmt.Sprintf("%s is required", name))
		}
		return v
	}
	opt := func(name, def string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}
		return def
	}

	c := &Config{
		Domain:             need("SPARKBASE_DOMAIN"),
		DatabaseURL:        need("DATABASE_URL"),
		RedisURL:           need("REDIS_URL"),
		TraefikConfigToken: need("TRAEFIK_CONFIG_TOKEN"),
		RedisPrefix:        opt("REDIS_PREFIX", "sb:"),
		NativeMode:         opt("NATIVE_MODE", "traefik"),
		TLSMode:            opt("TLS_MODE", "acme-dns"),
		ACMEEmail:          opt("ACME_EMAIL", ""),
		SMTPURL:            opt("SMTP_URL", ""),
		MailFrom:           opt("MAIL_FROM", ""),
		BootstrapPGHost:    opt("BOOTSTRAP_PG_HOST", ""),
		BootstrapCacheHost: opt("BOOTSTRAP_CACHE_HOST", ""),
		BootstrapStorage:   opt("BOOTSTRAP_STORAGE", ""),
		BackupDir:          opt("BACKUP_DIR", ""),
		Mode:               opt("SPARKBASE_MODE", "all"),
		LogLevel:           opt("LOG_LEVEL", "info"),
	}

	if key := need("ENCRYPTION_KEY"); key != "" {
		raw, err := base64.StdEncoding.DecodeString(key)
		if err != nil || len(raw) != 32 {
			problems = append(problems, "ENCRYPTION_KEY must be 32 bytes encoded as base64 (openssl rand -base64 32)")
		} else {
			c.EncryptionKey = raw
		}
	}

	for _, p := range []struct {
		name string
		def  string
		dst  *int
	}{{"NATIVE_PG_PORT", "5432", &c.NativePGPort}, {"NATIVE_PG_POOLED_PORT", "6432", &c.NativePGPooledPort}, {"NATIVE_REDIS_PORT", "6380", &c.NativeRedisPort}} {
		v, err := strconv.Atoi(opt(p.name, p.def))
		if err != nil || v <= 0 || v > 65535 {
			problems = append(problems, p.name+" must be a port number")
		}
		*p.dst = v
	}

	port, err := strconv.Atoi(opt("PORT", "8787"))
	if err != nil || port <= 0 || port > 65535 {
		problems = append(problems, "PORT must be a number between 1 and 65535")
	}
	c.Port = port

	switch c.NativeMode {
	case "traefik", "direct":
	default:
		problems = append(problems, "NATIVE_MODE must be traefik or direct")
	}
	switch c.TLSMode {
	case "acme-dns", "acme-http", "custom", "selfsigned", "none":
	default:
		problems = append(problems, "TLS_MODE must be acme-dns, acme-http, custom, selfsigned or none")
	}
	switch c.Mode {
	case "all", "api", "worker":
	default:
		problems = append(problems, "SPARKBASE_MODE must be all, api or worker")
	}

	scheme := "https"
	if c.Domain == "localhost" || strings.HasSuffix(c.Domain, ".localhost") {
		scheme = "http"
	}
	c.PublicAPIURL = strings.TrimRight(opt("PUBLIC_API_URL", scheme+"://api."+c.Domain), "/")
	c.DashboardURL = strings.TrimRight(opt("DASHBOARD_URL", scheme+"://app."+c.Domain), "/")
	c.SecureCookies = strings.HasPrefix(c.PublicAPIURL, "https://")
	if c.MailFrom == "" {
		c.MailFrom = "Sparkbase <no-reply@" + c.Domain + ">"
	}

	if len(problems) > 0 {
		return nil, errors.New("invalid configuration:\n  - " + strings.Join(problems, "\n  - "))
	}
	return c, nil
}

// ProjectURL is the base URL apps use for a project.
func (c *Config) ProjectURL(ref string) string {
	return c.PublicAPIURL + "/p/" + ref
}
