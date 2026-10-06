// Package tenant keeps connection pools to tenant services, opened with each
// service's own credentials so the project API has the same limits and
// isolation as a native connection.
package tenant

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shellhaki/sparkbase/server/internal/drivers"
	"github.com/shellhaki/sparkbase/server/internal/s3"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

type pgEntry struct {
	credID string
	pool   *pgxpool.Pool
	used   time.Time
}

type redisEntry struct {
	credID string
	client *redis.Client
	used   time.Time
}

type hostEntry struct {
	host *store.Host
	at   time.Time
}

type Pools struct {
	Store *store.Store
	Box   *sbcrypto.Box

	// Resolve returns a container service's host, waking it first. A nil
	// host means the service lives on a shared host.
	Resolve func(ctx context.Context, r *store.Resource) (*store.Host, error)

	mu    sync.Mutex
	pg    map[string]*pgEntry
	rd    map[string]*redisEntry
	hosts map[string]hostEntry
}

func New(s *store.Store, box *sbcrypto.Box) *Pools {
	return &Pools{Store: s, Box: box, pg: map[string]*pgEntry{}, rd: map[string]*redisEntry{}, hosts: map[string]hostEntry{}}
}

// Host returns a host, cached for 30 seconds.
func (p *Pools) Host(ctx context.Context, id string) (*store.Host, error) {
	p.mu.Lock()
	e, ok := p.hosts[id]
	p.mu.Unlock()
	if ok && time.Since(e.at) < 30*time.Second {
		return e.host, nil
	}
	h, err := p.Store.GetHost(ctx, id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.hosts[id] = hostEntry{host: h, at: time.Now()}
	p.mu.Unlock()
	return h, nil
}

// hostFor resolves where a service lives.
func (p *Pools) hostFor(ctx context.Context, r *store.Resource) (*store.Host, error) {
	if p.Resolve != nil {
		h, err := p.Resolve(ctx, r)
		if err != nil || h != nil {
			return h, err
		}
	}
	if r.HostID == nil {
		return nil, fmt.Errorf("service has no host")
	}
	return p.Host(ctx, *r.HostID)
}

// TenantURL is the connection string the API uses to reach a Postgres-family
// service over the internal network.
func TenantURL(h *store.Host, db, user, password string) string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(user, password), Host: h.InternalAddr, Path: "/" + db}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("application_name", "sparkbase-api")
	u.RawQuery = q.Encode()
	return u.String()
}

// PG returns a pool for a Postgres-family service.
func (p *Pools) PG(ctx context.Context, r *store.Resource) (*pgxpool.Pool, error) {
	cred, err := p.Store.CurrentCredential(ctx, r.ID)
	if err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}
	// Container services must be awake before a pooled connection is used.
	h, err := p.hostFor(ctx, r)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	e, ok := p.pg[r.ID]
	if ok && e.credID == cred.ID {
		e.used = time.Now()
		p.mu.Unlock()
		return e.pool, nil
	}
	p.mu.Unlock()
	secret, err := p.Box.OpenString(cred.SecretEnc)
	if err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(TenantURL(h, r.Ext(), cred.Username, secret))
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 3
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 2 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if old, ok := p.pg[r.ID]; ok {
		if old.credID == cred.ID {
			pool.Close()
			return old.pool, nil
		}
		go old.pool.Close()
	}
	p.pg[r.ID] = &pgEntry{credID: cred.ID, pool: pool, used: time.Now()}
	return pool, nil
}

// Redis returns a client for a cache service, logged in as its ACL user.
func (p *Pools) Redis(ctx context.Context, r *store.Resource) (*redis.Client, error) {
	cred, err := p.Store.CurrentCredential(ctx, r.ID)
	if err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}
	h, err := p.hostFor(ctx, r)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if e, ok := p.rd[r.ID]; ok && e.credID == cred.ID {
		e.used = time.Now()
		p.mu.Unlock()
		return e.client, nil
	}
	p.mu.Unlock()
	secret, err := p.Box.OpenString(cred.SecretEnc)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(&redis.Options{
		Addr: h.InternalAddr, Username: cred.Username, Password: secret,
		PoolSize: 3, DisableIdentity: true, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if old, ok := p.rd[r.ID]; ok {
		go old.client.Close()
	}
	p.rd[r.ID] = &redisEntry{credID: cred.ID, client: c, used: time.Now()}
	return c, nil
}

// Invalidate closes any pool for a service, e.g. after pause or delete.
func (p *Pools) Invalidate(resourceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.pg[resourceID]; ok {
		go e.pool.Close()
		delete(p.pg, resourceID)
	}
	if e, ok := p.rd[resourceID]; ok {
		go e.client.Close()
		delete(p.rd, resourceID)
	}
}

// CloseIdle closes pools unused for longer than idle.
func (p *Pools) CloseIdle(idle time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, e := range p.pg {
		if time.Since(e.used) > idle {
			go e.pool.Close()
			delete(p.pg, id)
		}
	}
	for id, e := range p.rd {
		if time.Since(e.used) > idle {
			go e.client.Close()
			delete(p.rd, id)
		}
	}
}

func (p *Pools) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.pg {
		e.pool.Close()
	}
	for _, e := range p.rd {
		e.client.Close()
	}
	p.pg, p.rd = map[string]*pgEntry{}, map[string]*redisEntry{}
}

// S3 returns an S3 client logged in with a Garage storage service's own key.
func (p *Pools) S3(ctx context.Context, r *store.Resource, h *store.Host) (*s3.Client, error) {
	cred, err := p.Store.CurrentCredential(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	secret, err := p.Box.OpenString(cred.SecretEnc)
	if err != nil {
		return nil, err
	}
	region := h.Config.Region
	if region == "" {
		region = "garage"
	}
	return &s3.Client{Endpoint: h.Config.S3Endpoint, Region: region, AccessKey: cred.Username, SecretKey: secret}, nil
}

// StorageDir returns the directory of a local storage service.
func (p *Pools) StorageDir(ctx context.Context, r *store.Resource) (string, *store.Host, error) {
	if r.HostID == nil {
		return "", nil, fmt.Errorf("service has no host")
	}
	h, err := p.Host(ctx, *r.HostID)
	if err != nil {
		return "", nil, err
	}
	return drivers.ServiceDir(h.Config.Root, r.ID), h, nil
}
