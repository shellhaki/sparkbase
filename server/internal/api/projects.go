package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shellhaki/sparkbase/server/internal/app"
	"github.com/shellhaki/sparkbase/server/internal/drivers"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

var slugRe = regexp.MustCompile(`^[a-z][a-z0-9]{5,19}$`)

func newSlug() string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	return string(letters[int(sbcrypto.RandomBytes(1)[0])%len(letters)]) + sbcrypto.RandomLower(9)
}

func (s *Server) projectJSON(p *store.Project) map[string]any {
	return map[string]any{
		"id": p.ID, "ref": p.Slug, "name": p.Name, "status": p.Status, "ownerId": p.OwnerID,
		"createdAt": p.CreatedAt, "url": s.Cfg.ProjectURL(p.Slug),
	}
}

// apiURL is the project API base for one service.
func (s *Server) apiURL(r *store.Resource) string {
	base := s.Cfg.ProjectURL(r.ProjectSlug)
	if r.Kind == "auth" {
		return base + "/auth"
	}
	return base + "/" + r.Kind + "/" + r.Name
}

func (s *Server) resourceJSON(r *store.Resource) map[string]any {
	return map[string]any{
		"id": r.ID, "projectId": r.ProjectID, "projectRef": r.ProjectSlug, "kind": r.Kind, "name": r.Name,
		"plan": r.PlanID, "limits": r.Limits, "status": r.Status, "writeLocked": r.WriteLocked,
		"endpoint": r.Endpoint, "lastError": r.LastError, "lastActiveAt": r.LastActiveAt,
		"createdAt": r.CreatedAt, "apiUrl": s.apiURL(r),
	}
}

func (s *Server) loadProject(r *http.Request, p *principal) (*store.Project, error) {
	proj, err := s.Store.GetProject(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && !p.canAccess(proj)) {
		return nil, httpx.NotFound("project")
	}
	return proj, err
}

func (s *Server) loadResource(r *http.Request, p *principal) (*store.Resource, error) {
	res, err := s.Store.GetResource(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		return nil, httpx.NotFound("service")
	}
	if err != nil {
		return nil, err
	}
	if p.Key != nil && p.Key.ProjectID != nil && *p.Key.ProjectID != res.ProjectID {
		return nil, httpx.NotFound("service")
	}
	if !p.User.IsAdmin() && res.OwnerID != p.User.ID {
		return nil, httpx.NotFound("service")
	}
	return res, nil
}

// createProjectKeys issues a publishable and a secret key.
func (s *Server) createProjectKey(ctx context.Context, tx *store.Store, projectID, kind string) error {
	prefix := sbcrypto.PrefixPublishable
	if kind == "secret" {
		prefix = sbcrypto.PrefixSecret
	}
	full, display := sbcrypto.NewKey(prefix)
	return tx.CreateProjectKey(ctx, projectID, kind, display, sbcrypto.SHA256Hex(full), s.Box.SealString(full))
}

func (s *Server) projectRoutes() {
	s.user("GET /v1/plans", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		plans, err := s.Store.ListPlans(r.Context(), r.URL.Query().Get("kind"), false)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"plans": plans})
	})

	s.user("GET /v1/projects", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		owner := p.User.ID
		if p.User.IsAdmin() && r.URL.Query().Get("all") == "true" {
			owner = ""
		}
		list, err := s.Store.ListProjects(r.Context(), owner)
		if err != nil {
			return err
		}
		out := []map[string]any{}
		for i := range list {
			if p.Key != nil && p.Key.ProjectID != nil && *p.Key.ProjectID != list[i].ID {
				continue
			}
			out = append(out, s.projectJSON(&list[i]))
		}
		return httpx.OK(w, map[string]any{"projects": out})
	})

	s.user("POST /v1/projects", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if p.Key != nil && p.Key.ProjectID != nil {
			return httpx.Forbidden("project-scoped keys cannot create projects")
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || len(in.Name) > 60 {
			return httpx.Invalid("name must be 1 to 60 characters")
		}
		if in.Slug == "" {
			in.Slug = newSlug()
		} else if !slugRe.MatchString(in.Slug) {
			return httpx.Invalid("ref must be 6 to 20 lowercase letters or digits, starting with a letter")
		}
		ctx := r.Context()
		var proj *store.Project
		err := s.Store.Tx(ctx, func(tx *store.Store) error {
			var err error
			proj, err = tx.CreateProject(ctx, p.User.ID, in.Slug, in.Name)
			if errors.Is(err, store.ErrConflict) {
				return httpx.Conflict("the ref " + in.Slug + " is taken")
			}
			if err != nil {
				return err
			}
			if err := s.createProjectKey(ctx, tx, proj.ID, "publishable"); err != nil {
				return err
			}
			return s.createProjectKey(ctx, tx, proj.ID, "secret")
		})
		if err != nil {
			return err
		}
		s.Audit(ctx, p.User.ID, proj.ID, "project.create", map[string]any{"ref": proj.Slug})
		return httpx.WriteJSON(w, 201, map[string]any{"project": s.projectJSON(proj)})
	})

	s.user("GET /v1/projects/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		proj, err := s.loadProject(r, p)
		if err != nil {
			return err
		}
		list, err := s.Store.ListResources(r.Context(), proj.ID)
		if err != nil {
			return err
		}
		res := make([]map[string]any, 0, len(list))
		for i := range list {
			res = append(res, s.resourceJSON(&list[i]))
		}
		return httpx.OK(w, map[string]any{"project": s.projectJSON(proj), "resources": res})
	})

	s.user("PATCH /v1/projects/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		proj, err := s.loadProject(r, p)
		if err != nil {
			return err
		}
		var in struct {
			Name string `json:"name"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || len(in.Name) > 60 {
			return httpx.Invalid("name must be 1 to 60 characters")
		}
		if err := s.Store.RenameProject(r.Context(), proj.ID, in.Name); err != nil {
			return err
		}
		proj.Name = in.Name
		return httpx.OK(w, map[string]any{"project": s.projectJSON(proj)})
	})

	s.user("DELETE /v1/projects/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		proj, err := s.loadProject(r, p)
		if err != nil {
			return err
		}
		ctx := r.Context()
		if err := s.Store.SetProjectStatus(ctx, proj.ID, "deleting"); err != nil {
			return err
		}
		if err := s.Enqueue(ctx, "delete_project", map[string]string{"projectId": proj.ID}, "delete_project:"+proj.ID); err != nil {
			return err
		}
		s.Audit(ctx, p.User.ID, proj.ID, "project.delete", nil)
		return httpx.WriteJSON(w, 202, map[string]any{"status": "deleting"})
	})

	s.user("GET /v1/projects/{id}/keys", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		proj, err := s.loadProject(r, p)
		if err != nil {
			return err
		}
		keys, err := s.Store.ActiveProjectKeys(r.Context(), proj.ID)
		if err != nil {
			return err
		}
		out := map[string]any{"url": s.Cfg.ProjectURL(proj.Slug)}
		for _, k := range keys {
			full, err := s.Box.OpenString(k.KeyEnc)
			if err != nil {
				return err
			}
			out[k.Kind+"Key"] = full
		}
		return httpx.OK(w, out)
	})

	s.user("POST /v1/projects/{id}/keys/rotate", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		proj, err := s.loadProject(r, p)
		if err != nil {
			return err
		}
		var in struct {
			Kind string `json:"kind"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if in.Kind != "publishable" && in.Kind != "secret" {
			return httpx.Invalid("kind must be publishable or secret")
		}
		ctx := r.Context()
		err = s.Store.Tx(ctx, func(tx *store.Store) error {
			if err := tx.RevokeProjectKeys(ctx, proj.ID, in.Kind); err != nil {
				return err
			}
			return s.createProjectKey(ctx, tx, proj.ID, in.Kind)
		})
		if err != nil {
			return err
		}
		s.Audit(ctx, p.User.ID, proj.ID, "project.key_rotate", map[string]any{"kind": in.Kind})
		return httpx.NoContent(w)
	})

	s.user("GET /v1/projects/{id}/resources", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		proj, err := s.loadProject(r, p)
		if err != nil {
			return err
		}
		list, err := s.Store.ListResources(r.Context(), proj.ID)
		if err != nil {
			return err
		}
		out := make([]map[string]any, 0, len(list))
		for i := range list {
			out = append(out, s.resourceJSON(&list[i]))
		}
		return httpx.OK(w, map[string]any{"resources": out})
	})

	s.user("POST /v1/projects/{id}/resources", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		proj, err := s.loadProject(r, p)
		if err != nil {
			return err
		}
		var in struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
			Plan string `json:"plan"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		res, err := s.CreateResource(r.Context(), p.User, proj, in.Kind, in.Name, in.Plan)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, 202, map[string]any{"resource": s.resourceJSON(res)})
	})

	s.user("GET /v1/resources/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"resource": s.resourceJSON(res)})
	})

	s.user("PATCH /v1/resources/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		var in struct {
			Name *string `json:"name"`
			Plan *string `json:"plan"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		if in.Name != nil && *in.Name != res.Name {
			if !app.ValidName(*in.Name) {
				return httpx.Invalid("name must be 1 to 31 lowercase letters, digits or dashes, starting with a letter")
			}
			if err := s.Store.SetResourceName(ctx, res.ID, *in.Name); errors.Is(err, store.ErrConflict) {
				return httpx.Conflict("a service with this name exists in the project")
			} else if err != nil {
				return err
			}
		}
		if in.Plan != nil && *in.Plan != res.PlanID {
			st, err := s.Settings(ctx)
			if err != nil {
				return err
			}
			if !p.User.IsAdmin() && !st.MembersChangePlans {
				return httpx.Forbidden("only admins can change plans on this install")
			}
			if err := s.ChangePlan(ctx, p.User.ID, res, *in.Plan); err != nil {
				return err
			}
			// Re-check limits under the new plan right away.
			if usage, err := s.Store.LatestUsage(ctx, res.ID); err == nil && len(usage) > 0 {
				if fresh, err := s.Store.GetResource(ctx, res.ID); err == nil {
					_ = s.EnforceLimits(ctx, fresh, usage)
				}
			}
		}
		res, err = s.Store.GetResource(ctx, res.ID)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"resource": s.resourceJSON(res)})
	})

	s.user("DELETE /v1/resources/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		if res.Status == "suspended" && !p.User.IsAdmin() {
			return httpx.Forbidden("suspended services can only be deleted by an admin")
		}
		if err := s.DeleteResource(r.Context(), p.User.ID, res); err != nil {
			return err
		}
		return httpx.WriteJSON(w, 202, map[string]any{"status": "deleting"})
	})

	for _, action := range []string{"pause", "resume"} {
		pause := action == "pause"
		s.user("POST /v1/resources/{id}/"+action, func(w http.ResponseWriter, r *http.Request, p *principal) error {
			res, err := s.loadResource(r, p)
			if err != nil {
				return err
			}
			if err := s.SetPaused(r.Context(), p.User.ID, res, pause); err != nil {
				return err
			}
			res, err = s.Store.GetResource(r.Context(), res.ID)
			if err != nil {
				return err
			}
			return httpx.OK(w, map[string]any{"resource": s.resourceJSON(res)})
		})
	}

	s.user("POST /v1/resources/{id}/retry", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		if err := s.RetryResource(r.Context(), p.User, res); err != nil {
			return err
		}
		return httpx.WriteJSON(w, 202, map[string]any{"status": "provisioning"})
	})

	s.user("POST /v1/resources/{id}/credentials/rotate", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		if err := s.Rotate(r.Context(), p.User.ID, res); err != nil {
			return err
		}
		return httpx.NoContent(w)
	})

	s.user("GET /v1/resources/{id}/connection", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		out, err := s.connectionInfo(r.Context(), res)
		if err != nil {
			return err
		}
		return httpx.OK(w, out)
	})

	s.user("GET /v1/resources/{id}/usage", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		ctx := r.Context()
		latest, err := s.Store.LatestUsage(ctx, res.ID)
		if err != nil {
			return err
		}
		if n, err := s.Counter.Get(ctx, res.ID); err == nil {
			latest["requests_month"] = n
		}
		to := time.Now()
		from := to.Add(-24 * time.Hour)
		if v, err := time.Parse(time.RFC3339, r.URL.Query().Get("from")); err == nil {
			from = v
		}
		if v, err := time.Parse(time.RFC3339, r.URL.Query().Get("to")); err == nil {
			to = v
		}
		series, err := s.Store.UsageSeries(ctx, res.ID, r.URL.Query().Get("metric"), from, to)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"usage": latest, "limits": res.Limits, "series": series,
			"over": app.OverLimits(res.Kind, latest, res.Limits)})
	})

	s.user("GET /v1/resources/{id}/backups", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		list, err := s.Store.ListBackups(r.Context(), res.ID)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"backups": list})
	})

	s.user("POST /v1/resources/{id}/backups", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		if !app.IsBackupKind(res.Kind) {
			return httpx.Invalid(res.Kind + " services have no database backups")
		}
		if res.Status != "active" && res.Status != "paused" {
			return httpx.NotReady(res.Status)
		}
		b, err := s.StartBackup(r.Context(), p.User.ID, res)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, 202, map[string]any{"backup": b})
	})

	s.user("POST /v1/resources/{id}/backups/{backupId}/restore", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		res, err := s.loadResource(r, p)
		if err != nil {
			return err
		}
		if res.Status != "active" {
			return httpx.NotReady(res.Status)
		}
		b, err := s.Store.GetBackup(r.Context(), r.PathValue("backupId"))
		if err != nil || b.ResourceID != res.ID {
			return httpx.NotFound("backup")
		}
		if b.Status != "done" {
			return httpx.Conflict("the backup is " + b.Status)
		}
		if err := s.RestoreBackup(r.Context(), p.User.ID, res, b); err != nil {
			return httpx.E(500, "restore_failed", err.Error())
		}
		return httpx.NoContent(w)
	})
}

// connectionInfo returns native connection details and the project API URL.
func (s *Server) connectionInfo(ctx context.Context, res *store.Resource) (map[string]any, error) {
	out := map[string]any{"kind": res.Kind, "apiUrl": s.apiURL(res), "projectUrl": s.Cfg.ProjectURL(res.ProjectSlug)}
	if res.Status == "provisioning" || res.Status == "failed" || res.Status == "deleting" {
		return nil, httpx.NotReady(res.Status)
	}
	cred, err := s.Store.CurrentCredential(ctx, res.ID)
	if err != nil {
		return nil, err
	}
	c, err := s.OpenCredential(cred)
	if err != nil {
		return nil, err
	}
	h, err := s.Tenants.Host(ctx, *res.HostID)
	if err != nil {
		return nil, err
	}
	switch res.Kind {
	case "postgres", "vector":
		host, port, tls := s.NativeAddr(res, h, false)
		phost, pport, _ := s.NativeAddr(res, h, true)
		ssl := "disable"
		if tls {
			ssl = "require"
		}
		mk := func(host string, port int) string {
			u := url.URL{Scheme: "postgres", User: url.UserPassword(c.Username, c.Secret),
				Host: fmt.Sprintf("%s:%d", host, port), Path: "/" + res.Ext(), RawQuery: "sslmode=" + ssl}
			return u.String()
		}
		out["native"] = map[string]any{
			"host": host, "port": port, "pooledPort": pport, "user": c.Username, "password": c.Secret,
			"database": res.Ext(), "sslmode": ssl, "url": mk(host, port), "pooledUrl": mk(phost, pport),
		}
	case "cache":
		host, port, tls := s.NativeAddr(res, h, false)
		if s.Cfg.NativeMode == "direct" {
			host, port = splitHostPort(h.InternalAddr, 6379)
			if h.Config.PublicHost != "" {
				host = h.Config.PublicHost
			}
		}
		scheme := "redis"
		if tls {
			scheme = "rediss"
		}
		u := url.URL{Scheme: scheme, User: url.UserPassword(c.Username, c.Secret), Host: fmt.Sprintf("%s:%d", host, port)}
		out["native"] = map[string]any{"host": host, "port": port, "user": c.Username, "password": c.Secret,
			"tls": tls, "url": u.String(), "keyPrefix": drivers.KeyPrefix(res.ID)}
	case "storage":
		out["bucket"] = res.Ext()
		out["backend"] = h.Config.Backend
		if h.Config.Backend == "garage" {
			endpoint := h.Config.S3Public
			if endpoint == "" {
				endpoint = h.Config.S3Endpoint
			}
			out["native"] = map[string]any{"endpoint": endpoint, "bucket": res.Ext(), "accessKeyId": c.Username,
				"secretAccessKey": c.Secret, "region": h.Config.Region, "forcePathStyle": true}
		}
	case "auth":
		out["jwtSecret"] = c.Extra
		out["jwtAlgorithm"] = "HS256"
	}
	return out, nil
}

func splitHostPort(addr string, def int) (string, int) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, def
	}
	var port int
	if _, err := fmt.Sscanf(addr[i+1:], "%d", &port); err != nil {
		return addr[:i], def
	}
	return addr[:i], port
}

// checkHost verifies a data host answers.
func (s *Server) checkHost(ctx context.Context, h *store.Host) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	secret, err := s.Box.OpenString(h.AdminSecretEnc)
	if err != nil {
		return err
	}
	switch h.Kind {
	case "postgres":
		conn, err := pgx.Connect(ctx, drivers.AdminURL(h, secret, ""))
		if err != nil {
			return errors.New("cannot connect")
		}
		return conn.Close(ctx)
	case "cache":
		rc := drivers.AdminClient(h.InternalAddr, secret)
		defer rc.Close()
		if rc.Ping(ctx).Err() != nil {
			return errors.New("cannot connect")
		}
	case "storage":
		if h.Config.Backend == "garage" {
			if (&drivers.GarageAdmin{URL: h.Config.AdminURL, Token: secret}).Ping(ctx) != nil {
				return errors.New("cannot reach the Garage admin API")
			}
		}
		if h.Config.Backend == "local" {
			if _, _, err := drivers.DirUsage(h.Config.Root); err != nil {
				return errors.New("root directory is not readable")
			}
		}
	}
	return nil
}
