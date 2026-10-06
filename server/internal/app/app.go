// Package app wires the dependencies shared by the API and the worker and
// holds the service lifecycle logic both of them use.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shellhaki/sparkbase/server/internal/config"
	"github.com/shellhaki/sparkbase/server/internal/core"
	"github.com/shellhaki/sparkbase/server/internal/db"
	"github.com/shellhaki/sparkbase/server/internal/drivers"
	"github.com/shellhaki/sparkbase/server/internal/mail"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
	"github.com/shellhaki/sparkbase/server/internal/tenant"
)

type App struct {
	Cfg     *config.Config
	Log     *slog.Logger
	Store   *store.Store
	Redis   *redis.Client
	Box     *sbcrypto.Box
	Mail    mail.Mailer
	Drivers drivers.Registry
	Limiter *core.RateLimiter
	Counter *core.Counter
	Tenants *tenant.Pools
	Metrics *Metrics
	// Now is the clock; tests may replace it.
	Now func() time.Time
}

// New connects to the control database and Redis, runs migrations and
// returns a ready App.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger, mailer mail.Mailer) (*App, error) {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	applied, err := db.Migrate(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if len(applied) > 0 {
		log.Info("migrations applied", "names", strings.Join(applied, ","))
	}
	ropts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	ropts.DisableIdentity = true
	rc := redis.NewClient(ropts)
	if err := rc.Ping(ctx).Err(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect redis: %w", err)
	}
	box, err := sbcrypto.NewBox(cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	if mailer == nil {
		if mailer, err = mail.New(cfg.SMTPURL, cfg.MailFrom, log); err != nil {
			return nil, err
		}
	}
	st := store.New(pool)
	a := &App{
		Cfg: cfg, Log: log, Store: st, Redis: rc, Box: box, Mail: mailer,
		Drivers: drivers.Registry{
			"postgres": drivers.NewPostgres(),
			"vector":   drivers.NewVector(),
			"queue":    drivers.NewQueue(),
			"auth":     drivers.NewAuth(),
			"cache":    drivers.NewCache(),
			"storage":  drivers.NewStorage(),
		},
		Limiter: &core.RateLimiter{R: rc, Prefix: cfg.RedisPrefix},
		Counter: &core.Counter{R: rc, Prefix: cfg.RedisPrefix},
		Tenants: tenant.New(st, box),
		Metrics: NewMetrics(),
		Now:     time.Now,
	}
	if err := a.BootstrapHosts(ctx); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

func (a *App) Close() {
	a.Tenants.Close()
	a.Redis.Close()
	a.Store.Pool.Close()
}

// BootstrapHosts registers the bundled data hosts from the environment once.
func (a *App) BootstrapHosts(ctx context.Context) error {
	if a.Cfg.BootstrapPGHost != "" {
		u, err := url.Parse(a.Cfg.BootstrapPGHost)
		if err != nil || u.User == nil {
			return fmt.Errorf("BOOTSTRAP_PG_HOST must be postgres://admin:password@host:port/db")
		}
		pass, _ := u.User.Password()
		cfg := store.HostConfig{AdminUser: u.User.Username(), AdminDB: strings.TrimPrefix(u.Path, "/"),
			PublicHost: u.Query().Get("public_host"), PgBinDir: u.Query().Get("pg_bin"), PoolerAddr: u.Query().Get("pooler")}
		if p := u.Query().Get("public_port"); p != "" {
			fmt.Sscanf(p, "%d", &cfg.PublicPort)
		}
		if _, err := a.RegisterHost(ctx, "postgres", "pg-1", u.Host, cfg, pass, 100); err != nil && err != store.ErrConflict {
			return err
		}
	}
	if a.Cfg.BootstrapCacheHost != "" {
		u, err := url.Parse(a.Cfg.BootstrapCacheHost)
		if err != nil {
			return fmt.Errorf("BOOTSTRAP_CACHE_HOST must be redis://:password@host:port")
		}
		pass := ""
		if u.User != nil {
			pass, _ = u.User.Password()
		}
		cfg := store.HostConfig{PublicHost: u.Query().Get("public_host")}
		if _, err := a.RegisterHost(ctx, "cache", "cache-1", u.Host, cfg, pass, 100); err != nil && err != store.ErrConflict {
			return err
		}
	}
	if a.Cfg.BootstrapStorage != "" {
		u, err := url.Parse(a.Cfg.BootstrapStorage)
		switch {
		case err == nil && u.Scheme == "local":
			cfg := store.HostConfig{Backend: "local", Root: u.Path}
			if _, err := a.RegisterHost(ctx, "storage", "storage-1", "local", cfg, sbcrypto.RandomString(32), 100); err != nil && err != store.ErrConflict {
				return err
			}
		case err == nil && u.Scheme == "garage" && u.User != nil:
			// garage://ADMIN_TOKEN@garage:3903?s3=http://garage:3900&public=https://s3.example.com&region=garage
			q := u.Query()
			scheme := "http"
			if q.Get("tls") == "1" {
				scheme = "https"
			}
			cfg := store.HostConfig{Backend: "garage", AdminURL: scheme + "://" + u.Host, S3Endpoint: q.Get("s3"), S3Public: q.Get("public"), Region: q.Get("region")}
			if cfg.S3Endpoint == "" {
				return fmt.Errorf("BOOTSTRAP_STORAGE garage URL needs ?s3=http://garage:3900")
			}
			if cfg.Region == "" {
				cfg.Region = "garage"
			}
			if _, err := a.RegisterHost(ctx, "storage", "storage-1", cfg.AdminURL, cfg, u.User.Username(), 100); err != nil && err != store.ErrConflict {
				return err
			}
		default:
			return fmt.Errorf("BOOTSTRAP_STORAGE must be local:///absolute/path or garage://ADMIN_TOKEN@host:3903?s3=http://host:3900")
		}
	}
	return nil
}

// RegisterHost stores a host and prepares it for tenants.
func (a *App) RegisterHost(ctx context.Context, kind, name, addr string, cfg store.HostConfig, secret string, weight int) (*store.Host, error) {
	if existing, err := a.Store.GetHostByName(ctx, name); err == nil {
		if existing.BootstrappedAt == nil {
			return existing, a.BootstrapHost(ctx, existing)
		}
		return existing, store.ErrConflict
	}
	h, err := a.Store.CreateHost(ctx, kind, name, addr, cfg, a.Box.SealString(secret), weight)
	if err != nil {
		return nil, err
	}
	return h, a.BootstrapHost(ctx, h)
}

// BootstrapHost locks a new host down for tenants.
func (a *App) BootstrapHost(ctx context.Context, h *store.Host) error {
	secret, err := a.Box.OpenString(h.AdminSecretEnc)
	if err != nil {
		return err
	}
	switch h.Kind {
	case "postgres":
		hasVector, err := drivers.BootstrapPGHost(ctx, h, secret)
		if err != nil {
			return fmt.Errorf("bootstrap host %s: %w", h.Name, err)
		}
		cfg := h.Config
		cfg.HasVector = hasVector
		if err := a.Store.UpdateHost(ctx, h.ID, nil, nil, &cfg); err != nil {
			return err
		}
	case "storage":
		if h.Config.Backend == "garage" {
			g := &drivers.GarageAdmin{URL: h.Config.AdminURL, Token: secret}
			var err error
			for i := 0; i < 30; i++ {
				if err = g.BootstrapLayout(ctx, 1<<40); err == nil {
					break
				}
				time.Sleep(time.Second)
			}
			if err != nil {
				return fmt.Errorf("bootstrap host %s: %w", h.Name, err)
			}
		}
	case "cache":
		rc := drivers.AdminClient(h.InternalAddr, secret)
		defer rc.Close()
		if err := rc.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("bootstrap host %s: %w", h.Name, err)
		}
	}
	return a.Store.MarkHostBootstrapped(ctx, h.ID)
}

// Target loads what a driver needs to act on a service.
func (a *App) Target(ctx context.Context, r *store.Resource) (*drivers.Target, drivers.Driver, error) {
	d, err := a.Drivers.Get(r.Kind)
	if err != nil {
		return nil, nil, err
	}
	if r.HostID == nil {
		return nil, nil, fmt.Errorf("service %s has no host", r.ID)
	}
	h, err := a.Store.GetHost(ctx, *r.HostID)
	if err != nil {
		return nil, nil, err
	}
	secret, err := a.Box.OpenString(h.AdminSecretEnc)
	if err != nil {
		return nil, nil, err
	}
	state := map[string]any{}
	if len(r.ProvisionState) > 0 {
		_ = json.Unmarshal(r.ProvisionState, &state)
	}
	id := r.ID
	t := &drivers.Target{R: r, H: h, AdminSecret: secret, Limits: r.Limits, State: state,
		Save: func(s map[string]any) error { return a.Store.SetProvisionState(context.Background(), id, s) }}
	return t, d, nil
}

// Credentials returns every credential of a service that is not revoked yet.
func (a *App) Credentials(ctx context.Context, resourceID string) ([]drivers.Credentials, error) {
	list, err := a.Store.ListCredentials(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	out := make([]drivers.Credentials, 0, len(list))
	for _, c := range list {
		cr, err := a.OpenCredential(&c)
		if err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	return out, nil
}

func (a *App) OpenCredential(c *store.Credential) (drivers.Credentials, error) {
	secret, err := a.Box.OpenString(c.SecretEnc)
	if err != nil {
		return drivers.Credentials{}, err
	}
	cr := drivers.Credentials{Username: c.Username, Secret: secret}
	if len(c.ExtraEnc) > 0 {
		if cr.Extra, err = a.Box.OpenString(c.ExtraEnc); err != nil {
			return drivers.Credentials{}, err
		}
	}
	return cr, nil
}

// Enqueue adds a job and logs failures instead of hiding them.
func (a *App) Enqueue(ctx context.Context, typ string, payload any, dedupe string) error {
	_, err := a.Store.Enqueue(ctx, store.NewJob{Type: typ, Payload: payload, DedupeKey: dedupe})
	return err
}

// NotifyOwner writes a dashboard notification and emails the owner when SMTP is set.
func (a *App) NotifyOwner(ctx context.Context, r *store.Resource, level, message string) {
	if err := a.Store.Notify(ctx, r.OwnerID, r.ID, level, message); err != nil {
		a.Log.Error("notify", "err", err.Error())
	}
	if a.Mail.Configured() {
		if u, err := a.Store.GetUser(ctx, r.OwnerID); err == nil {
			_ = a.Enqueue(ctx, "send_email", map[string]string{
				"to": u.Email, "subject": "Sparkbase: " + message,
				"text": message + "\n\nOpen the dashboard: " + a.Cfg.DashboardURL + "\n",
				"link": a.Cfg.DashboardURL,
			}, "")
		}
	}
}

// Metrics is a small Prometheus text-format registry.
type Metrics struct {
	mu       sync.Mutex
	requests map[string]int64
	latency  map[string][2]float64 // sum seconds, count
	gauges   map[string]float64
}

func NewMetrics() *Metrics {
	return &Metrics{requests: map[string]int64{}, latency: map[string][2]float64{}, gauges: map[string]float64{}}
}

func (m *Metrics) ObserveRequest(method, route string, status int, d time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	k := fmt.Sprintf(`route=%q,status="%d"`, route, status)
	m.requests[k]++
	l := m.latency[fmt.Sprintf(`route=%q`, route)]
	l[0] += d.Seconds()
	l[1]++
	m.latency[fmt.Sprintf(`route=%q`, route)] = l
}

func (m *Metrics) SetGauge(name string, v float64) {
	m.mu.Lock()
	m.gauges[name] = v
	m.mu.Unlock()
}

func (m *Metrics) Render() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b strings.Builder
	b.WriteString("# TYPE sparkbase_http_requests_total counter\n")
	for k, v := range m.requests {
		fmt.Fprintf(&b, "sparkbase_http_requests_total{%s} %d\n", k, v)
	}
	b.WriteString("# TYPE sparkbase_http_request_seconds summary\n")
	for k, v := range m.latency {
		fmt.Fprintf(&b, "sparkbase_http_request_seconds_sum{%s} %f\n", k, v[0])
		fmt.Fprintf(&b, "sparkbase_http_request_seconds_count{%s} %.0f\n", k, v[1])
	}
	for k, v := range m.gauges {
		fmt.Fprintf(&b, "%s %g\n", k, v)
	}
	return b.String()
}
