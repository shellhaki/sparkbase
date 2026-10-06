// Package engine runs memory-heavy services (Postgres family and Redis) in
// their own Docker containers. Containers start on demand and stop after a
// quiet period, so a service only holds memory while it is in use.
package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/shellhaki/sparkbase/server/internal/dockerx"
	"github.com/shellhaki/sparkbase/server/internal/drivers"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

const (
	PGImage    = "pgvector/pgvector:pg18"
	RedisImage = "redis:8-alpine"
)

var ErrNotRunnable = errors.New("the service is not active")

// Engine creates, wakes and stops service containers.
type Engine struct {
	Docker  *dockerx.Client
	Network string
	// InDocker is true when Sparkbase itself runs in a container on Network:
	// containers are reached by name. Otherwise each container publishes its
	// port on 127.0.0.1.
	InDocker  bool
	ProxyBase int
	Box       *sbcrypto.Box
	Store     *store.Store

	mu      sync.Mutex
	locks   map[string]*sync.Mutex
	running map[string]time.Time // recently seen running, to skip inspects
}

// Uses reports whether a kind runs in its own container.
func Uses(kind string) bool {
	switch kind {
	case "postgres", "vector", "queue", "auth", "cache":
		return true
	}
	return false
}

// InsideDocker guesses whether this process runs in a container.
func InsideDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func (e *Engine) lock(id string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.locks == nil {
		e.locks = map[string]*sync.Mutex{}
	}
	l, ok := e.locks[id]
	if !ok {
		l = &sync.Mutex{}
		e.locks[id] = l
	}
	return l
}

func ContainerName(r *store.Resource) string { return "sb-" + strings.ReplaceAll(r.ID, "-", "")[:20] }
func volumeName(r *store.Resource) string    { return ContainerName(r) + "-data" }

// hostPort is the published port in non-Docker mode, stable across restarts.
func (e *Engine) hostPort(r *store.Resource) int {
	if r.ProxyPort == nil {
		return 0
	}
	return *r.ProxyPort + 1000
}

func servicePort(kind string) int {
	if kind == "cache" {
		return 6379
	}
	return 5432
}

// Addr is where Sparkbase reaches the container.
func (e *Engine) Addr(r *store.Resource) string {
	if e.InDocker {
		return net.JoinHostPort(ContainerName(r), strconv.Itoa(servicePort(r.Kind)))
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(e.hostPort(r)))
}

// Host is a per-service host the drivers can work against.
func (e *Engine) Host(r *store.Resource) *store.Host {
	kind := "postgres"
	if r.Kind == "cache" {
		kind = "cache"
	}
	return &store.Host{
		ID: "container:" + r.ID, Kind: kind, Name: ContainerName(r), InternalAddr: e.Addr(r),
		Config: store.HostConfig{AdminUser: "postgres", AdminDB: "postgres", HasVector: kind == "postgres"},
		Status: "active",
	}
}

func (e *Engine) AdminSecret(r *store.Resource) (string, error) {
	if len(r.AdminSecretEnc) == 0 {
		return "", errors.New("container has no admin secret")
	}
	return e.Box.OpenString(r.AdminSecretEnc)
}

func (e *Engine) spec(r *store.Resource, secret string) dockerx.Spec {
	limits := r.Limits
	labels := map[string]string{"sparkbase": "1", "sparkbase.resource": r.ID, "sparkbase.kind": r.Kind, "sparkbase.project": r.ProjectSlug, "sparkbase.network": e.Network}
	mem := limits.Get("memory_mb")
	s := dockerx.Spec{Name: ContainerName(r), Labels: labels, Network: e.Network, Volume: volumeName(r)}
	if !e.InDocker {
		s.PublishHost = "127.0.0.1"
		s.PublishPort = e.hostPort(r)
		s.ContainerPort = servicePort(r.Kind)
	}
	if r.Kind == "cache" {
		if mem <= 0 {
			mem = 32
		}
		s.Image = RedisImage
		s.MountPath = "/data"
		s.MemoryBytes = (mem + 48) << 20
		s.Env = []string{"SB_PASSWORD=" + secret}
		s.Cmd = []string{"sh", "-c", fmt.Sprintf(`[ -s /data/users.acl ] || echo "user default on >$SB_PASSWORD ~* &* +@all" > /data/users.acl; exec redis-server --aclfile /data/users.acl --appendonly yes --dir /data --maxmemory %dmb --maxmemory-policy noeviction`, mem)}
		return s
	}
	if mem <= 0 {
		mem = 256
	}
	conns := limits.Get("direct_connections")
	if conns <= 0 {
		conns = 5
	}
	s.Image = PGImage
	s.MountPath = "/var/lib/postgresql"
	s.MemoryBytes = mem << 20
	s.Env = []string{"POSTGRES_PASSWORD=" + secret, "POSTGRES_USER=postgres", "POSTGRES_DB=postgres", "POSTGRES_INITDB_ARGS=--auth=scram-sha-256"}
	shared := mem / 4
	if shared < 16 {
		shared = 16
	}
	s.Cmd = []string{"postgres",
		"-c", fmt.Sprintf("shared_buffers=%dMB", shared),
		"-c", fmt.Sprintf("max_connections=%d", conns+15),
		"-c", fmt.Sprintf("effective_cache_size=%dMB", mem/2),
		"-c", "maintenance_work_mem=16MB",
		"-c", "log_min_duration_statement=1000",
		"-c", "log_connections=on",
		"-c", "log_disconnections=on",
	}
	return s
}

// Prepare gives a service a container name, admin secret and proxy port.
func (e *Engine) Prepare(ctx context.Context, r *store.Resource) error {
	if r.ContainerName != nil && r.ProxyPort != nil && len(r.AdminSecretEnc) > 0 {
		return nil
	}
	for i := 0; i < 5; i++ {
		port, err := e.Store.NextProxyPort(ctx, e.ProxyBase)
		if err != nil {
			return err
		}
		secret := sbcrypto.RandomString(32)
		err = e.Store.SetContainer(ctx, r.ID, ContainerName(r), e.Box.SealString(secret), port)
		if store.IsUniqueViolation(err) {
			continue
		}
		if err != nil {
			return err
		}
		fresh, err := e.Store.GetResource(ctx, r.ID)
		if err != nil {
			return err
		}
		*r = *fresh
		return nil
	}
	return errors.New("could not allocate a proxy port")
}

// Ensure creates the container if needed, starts it and waits until the
// service accepts connections.
func (e *Engine) Ensure(ctx context.Context, r *store.Resource) error {
	l := e.lock(r.ID)
	l.Lock()
	defer l.Unlock()
	secret, err := e.AdminSecret(r)
	if err != nil {
		return err
	}
	st, err := e.Docker.Inspect(ctx, ContainerName(r))
	if err != nil {
		return err
	}
	if !st.Exists {
		spec := e.spec(r, secret)
		if err := e.Docker.EnsureImage(ctx, spec.Image); err != nil {
			return err
		}
		if e.Network != "" {
			if err := e.Docker.EnsureNetwork(ctx, e.Network); err != nil {
				return err
			}
		}
		if err := e.Docker.EnsureVolume(ctx, volumeName(r), spec.Labels); err != nil {
			return err
		}
		if err := e.Docker.Create(ctx, spec); err != nil && !strings.Contains(err.Error(), "already in use") {
			return err
		}
	}
	if !st.Running {
		if err := e.Docker.Start(ctx, ContainerName(r)); err != nil {
			return err
		}
	}
	return e.waitReady(ctx, r, secret)
}

func (e *Engine) waitReady(ctx context.Context, r *store.Resource, secret string) error {
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if r.Kind == "cache" {
			rc := drivers.AdminClient(e.Addr(r), secret)
			last = rc.Ping(cctx).Err()
			rc.Close()
		} else {
			var conn *pgx.Conn
			conn, last = pgx.Connect(cctx, drivers.AdminURL(e.Host(r), secret, "postgres"))
			if last == nil {
				conn.Close(cctx)
			}
		}
		cancel()
		if last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("the service did not become ready: %v", last)
}

// Running reports whether the container is up.
func (e *Engine) Running(ctx context.Context, r *store.Resource) (bool, error) {
	st, err := e.Docker.Inspect(ctx, ContainerName(r))
	return st.Running, err
}

// Wake starts a sleeping service. Paused and suspended services stay down.
func (e *Engine) Wake(ctx context.Context, r *store.Resource) error {
	if r.Status != "active" && r.Status != "provisioning" {
		return ErrNotRunnable
	}
	e.mu.Lock()
	seen, ok := e.running[r.ID]
	e.mu.Unlock()
	if ok && time.Since(seen) < 5*time.Second {
		return nil
	}
	if running, err := e.Running(ctx, r); err == nil && running {
		e.markRunning(r.ID)
		return nil
	}
	if err := e.Ensure(ctx, r); err != nil {
		return err
	}
	e.markRunning(r.ID)
	return nil
}

func (e *Engine) markRunning(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running == nil {
		e.running = map[string]time.Time{}
	}
	e.running[id] = time.Now()
}

// Sleep stops the container; data stays in its volume.
func (e *Engine) Sleep(ctx context.Context, r *store.Resource) error {
	e.mu.Lock()
	delete(e.running, r.ID)
	e.mu.Unlock()
	return e.Docker.Stop(ctx, ContainerName(r), 20*time.Second)
}

// Destroy removes the container and its data.
func (e *Engine) Destroy(ctx context.Context, r *store.Resource) error {
	if err := e.Docker.Remove(ctx, ContainerName(r)); err != nil {
		return err
	}
	return e.Docker.RemoveVolume(ctx, volumeName(r))
}

// RedisFor is a convenience for health checks.
func RedisFor(addr, secret string) *redis.Client { return drivers.AdminClient(addr, secret) }
