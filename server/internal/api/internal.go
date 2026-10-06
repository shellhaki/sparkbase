package api

import (
	"crypto/subtle"
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
		routers := map[string]any{}
		services := map[string]any{}
		for i := range list {
			res := &list[i]
			if res.Endpoint == nil || *res.Endpoint == "" || res.HostID == nil {
				continue
			}
			h, err := s.Tenants.Host(ctx, *res.HostID)
			if err != nil {
				continue
			}
			sni := "HostSNI(`" + *res.Endpoint + "`)"
			id := strings.ReplaceAll(res.ID, "-", "")[:16]
			switch res.Kind {
			case "postgres", "vector":
				routers["pg-"+id] = map[string]any{"entryPoints": []string{"postgres"}, "rule": sni, "service": "pgsvc-" + id, "tls": map[string]any{}}
				services["pgsvc-"+id] = map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]string{"address": h.InternalAddr}}}}
				pooled := h.InternalAddr
				if h.Config.PoolerAddr != "" {
					pooled = h.Config.PoolerAddr
				}
				routers["pgp-"+id] = map[string]any{"entryPoints": []string{"postgres-pooled"}, "rule": sni, "service": "pgpsvc-" + id, "tls": map[string]any{}}
				services["pgpsvc-"+id] = map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]string{"address": pooled}}}}
			case "cache":
				routers["rd-"+id] = map[string]any{"entryPoints": []string{"redis"}, "rule": sni, "service": "rdsvc-" + id, "tls": map[string]any{}}
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
