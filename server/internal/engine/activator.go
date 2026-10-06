package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

// Activator listens on one internal port per container service. A client
// connection wakes the container if it sleeps, then traffic is piped
// through. Traefik routes each service's hostname to its activator port.
type Activator struct {
	Engine *Engine
	Store  *store.Store
	Redis  *redis.Client
	Prefix string
	Log    *slog.Logger
	// BindHost is the interface to listen on: 0.0.0.0 inside Docker,
	// 127.0.0.1 on a developer machine.
	BindHost string

	mu        sync.Mutex
	listeners map[string]net.Listener // resource id -> listener
}

func (a *Activator) connKey(id string) string { return a.Prefix + "conns:" + id }

// ActiveConns is the number of open client connections to a service.
func ActiveConns(ctx context.Context, rc *redis.Client, prefix, id string) int64 {
	n, _ := rc.Get(ctx, prefix+"conns:"+id).Int64()
	return n
}

// Sync opens listeners for every container service and closes the rest.
func (a *Activator) Sync(ctx context.Context) error {
	list, err := a.Store.ContainerResources(ctx)
	if err != nil {
		return err
	}
	want := map[string]store.Resource{}
	for _, r := range list {
		if r.ProxyPort != nil && r.Status != "deleting" && (r.Kind != "queue" && r.Kind != "auth") {
			want[r.ID] = r
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.listeners == nil {
		a.listeners = map[string]net.Listener{}
	}
	for id, l := range a.listeners {
		if _, ok := want[id]; !ok {
			l.Close()
			delete(a.listeners, id)
		}
	}
	for id, r := range want {
		if _, ok := a.listeners[id]; ok {
			continue
		}
		l, err := net.Listen("tcp", net.JoinHostPort(a.BindHost, strconv.Itoa(*r.ProxyPort)))
		if err != nil {
			a.Log.Warn("activator listen", "resource", id, "err", err.Error())
			continue
		}
		a.listeners[id] = l
		go a.serve(l, id)
	}
	return nil
}

// Run keeps listeners in sync until ctx ends.
func (a *Activator) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		if err := a.Sync(ctx); err != nil && ctx.Err() == nil {
			a.Log.Warn("activator sync", "err", err.Error())
		}
		select {
		case <-ctx.Done():
			a.mu.Lock()
			for _, l := range a.listeners {
				l.Close()
			}
			a.mu.Unlock()
			return
		case <-t.C:
		}
	}
}

func (a *Activator) serve(l net.Listener, id string) {
	for {
		c, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go a.handle(c, id)
	}
}

func (a *Activator) handle(client net.Conn, id string) {
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	r, err := a.Store.GetResource(ctx, id)
	if err != nil {
		cancel()
		return
	}
	// Paused and suspended services are refused here; the client sees the
	// connection close.
	if err := a.Engine.Wake(ctx, r); err != nil {
		cancel()
		a.Log.Info("activator refused", "resource", id, "reason", err.Error())
		return
	}
	cancel()
	upstream, err := net.DialTimeout("tcp", a.Engine.Addr(r), 10*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()

	bg := context.Background()
	a.Redis.Incr(bg, a.connKey(id))
	a.Redis.Expire(bg, a.connKey(id), 24*time.Hour)
	defer a.Redis.Decr(bg, a.connKey(id))
	_ = a.Store.TouchResource(bg, id)

	// Keep the service marked active while the connection is open.
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				_ = a.Store.TouchResource(bg, id)
			}
		}
	}()
	defer close(done)

	errc := make(chan error, 2)
	go func() { _, err := io.Copy(upstream, client); errc <- err }()
	go func() { _, err := io.Copy(client, upstream); errc <- err }()
	<-errc
}
