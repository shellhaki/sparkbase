package api

import (
	"net"
	"strconv"

	"crypto/subtle"
	"github.com/shellhaki/sparkbase/server/internal/store"
	"net/http"
	"strings"

	"github.com/shellhaki/sparkbase/server/internal/httpx"
)

func (s *Server) internalToken(r *http.Request) bool {
	tok := bearer(r)
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	return tok != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(s.Cfg.TraefikConfigToken)) == 1
}

func (s *Server) internalRoutes() {
	// Traefik's HTTP provider polls this for one TCP router per running
	// native service. Paused and deleted services get no router.
	s.handle("GET /internal/traefik/config", func(w http.ResponseWriter, r *http.Request) error {
		if !s.internalToken(r) {
			return httpx.Unauthorized("token required")
		}
		ctx := r.Context()
		list, err := s.Store.ListResourcesByStatus(ctx, "active")
		if err != nil {
			return err
		}
		// TLS settings per certificate mode: wildcard certificates through
		// DNS-01, one certificate per hostname through HTTP-01, or the
		// default/file certificates for custom and selfsigned.
		tlsFor := func(wildcard string) map[string]any {
			switch s.Cfg.TLSMode {
			case "acme-dns":
				return map[string]any{"certResolver": "le", "domains": []any{map[string]any{"main": wildcard}}}
			case "acme-http":
				return map[string]any{"certResolver": "le"}
			}
			return map[string]any{}
		}
		dbTLS, cacheTLS := tlsFor("*.db."+s.Cfg.Domain), tlsFor("*.cache."+s.Cfg.Domain)
		routers := map[string]any{}
		services := map[string]any{}
		for i := range list {
			res := &list[i]
			if res.Endpoint == nil || *res.Endpoint == "" {
				continue
			}
			var h *store.Host
			if res.Runtime == "container" && res.ProxyPort != nil {
				// Container services go through the activator, which wakes them.
				addr := net.JoinHostPort(s.Cfg.ActivatorHost, strconv.Itoa(*res.ProxyPort))
				h = &store.Host{InternalAddr: addr}
			} else if res.HostID != nil {
				var err error
				if h, err = s.Tenants.Host(ctx, *res.HostID); err != nil {
					continue
				}
			} else {
				continue
			}
			sni := "HostSNI(`" + *res.Endpoint + "`)"
			id := strings.ReplaceAll(res.ID, "-", "")[:16]
			switch res.Kind {
			case "postgres", "vector":
				routers["pg-"+id] = map[string]any{"entryPoints": []string{"postgres"}, "rule": sni, "service": "pgsvc-" + id, "tls": dbTLS}
				services["pgsvc-"+id] = map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]string{"address": h.InternalAddr}}}}
				pooled := h.InternalAddr
				if h.Config.PoolerAddr != "" {
					pooled = h.Config.PoolerAddr
				}
				routers["pgp-"+id] = map[string]any{"entryPoints": []string{"postgres-pooled"}, "rule": sni, "service": "pgpsvc-" + id, "tls": dbTLS}
				services["pgpsvc-"+id] = map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]string{"address": pooled}}}}
			case "cache":
				routers["rd-"+id] = map[string]any{"entryPoints": []string{"redis"}, "rule": sni, "service": "rdsvc-" + id, "tls": cacheTLS}
				services["rdsvc-"+id] = map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]string{"address": h.InternalAddr}}}}
			}
		}
		out := map[string]any{}
		if len(routers) > 0 {
			out["tcp"] = map[string]any{"routers": routers, "services": services}
		}
		return httpx.OK(w, out)
	})

	s.handle("GET /metrics", func(w http.ResponseWriter, r *http.Request) error {
		if !s.internalToken(r) {
			return httpx.Unauthorized("token required")
		}
		if st, err := s.Store.JobStats(r.Context()); err == nil {
			s.Metrics.SetGauge(`sparkbase_jobs{state="queued"}`, float64(st.Queued))
			s.Metrics.SetGauge(`sparkbase_jobs{state="running"}`, float64(st.Running))
			s.Metrics.SetGauge(`sparkbase_jobs_failed_24h`, float64(st.Failed))
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, err := w.Write([]byte(s.Metrics.Render()))
		return err
	})
}
