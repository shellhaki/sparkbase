package api

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/httpx"
	sbmail "github.com/shellhaki/sparkbase/server/internal/mail"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

var planIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,40}$`)

// knownLimits lists the limit names each kind understands.
var knownLimits = map[string][]string{
	"postgres": {"storage_mb", "direct_connections", "pooled_connections", "statement_timeout_ms", "work_mem_mb", "requests_per_month", "backup_days"},
	"vector":   {"storage_mb", "max_dimension", "direct_connections", "statement_timeout_ms", "work_mem_mb", "requests_per_month", "backup_days"},
	"queue":    {"storage_mb", "max_message_kb", "direct_connections", "statement_timeout_ms", "work_mem_mb", "requests_per_month", "backup_days"},
	"auth":     {"max_users", "storage_mb", "direct_connections", "statement_timeout_ms", "work_mem_mb", "requests_per_month", "backup_days"},
	"cache":    {"memory_mb", "connections", "requests_per_month"},
	"storage":  {"storage_mb", "max_upload_mb", "requests_per_month"},
}

func validLimits(kind string, l store.Limits) error {
	allowed := map[string]bool{}
	for _, k := range knownLimits[kind] {
		allowed[k] = true
	}
	for k, v := range l {
		if !allowed[k] {
			return httpx.Invalid("unknown limit " + k + " for kind " + kind)
		}
		if v < -1 || v == 0 {
			return httpx.Invalid("limit " + k + " must be positive, or -1 for unlimited")
		}
	}
	return nil
}

func (s *Server) adminRoutes() {
	s.admin("GET /v1/admin/users", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		users, err := s.Store.ListUsers(r.Context())
		if err != nil {
			return err
		}
		invites, err := s.Store.ListPendingInvites(r.Context())
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"users": users, "invites": invites})
	})

	s.admin("POST /v1/admin/invites", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		email, ok := NormalizeEmail(in.Email)
		if !ok {
			return httpx.Invalid("enter a valid email address")
		}
		if in.Role == "" {
			in.Role = "member"
		}
		if in.Role != "member" && in.Role != "admin" {
			return httpx.Invalid("role must be member or admin")
		}
		if _, err := s.Store.GetUserByEmail(ctx, email); err == nil {
			return httpx.Conflict("a user with this email already exists")
		}
		raw := sbcrypto.Token()
		inv, err := s.Store.CreateAuthToken(ctx, store.NewAuthToken{
			Kind: "invite", Email: &email, Role: &in.Role, InvitedBy: &p.User.ID,
			TokenHash: sbcrypto.SHA256Hex(raw), ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		})
		if err != nil {
			return err
		}
		link := s.Cfg.DashboardURL + "/invite?token=" + raw
		emailed := s.Mail.Configured()
		if err := s.Mail.Send(ctx, sbmail.Message{
			To: email, Subject: "You're invited to Sparkbase", Link: link,
			Text: "You have been invited to a Sparkbase install as " + in.Role + ".\n\nAccept the invite:\n\n" + link + "\n\nThe link expires in 7 days.\n",
		}); err != nil {
			s.Log.Warn("invite email failed", "err", err.Error())
			emailed = false
		}
		s.Audit(ctx, p.User.ID, "", "invite.create", map[string]any{"invite": inv.ID, "role": in.Role})
		return httpx.WriteJSON(w, 201, map[string]any{"invite": inv, "link": link, "emailed": emailed})
	})

	s.admin("DELETE /v1/admin/invites/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		ok, err := s.Store.DeleteInvite(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		if !ok {
			return httpx.NotFound("invite")
		}
		s.Audit(r.Context(), p.User.ID, "", "invite.cancel", map[string]any{"invite": r.PathValue("id")})
		return httpx.NoContent(w)
	})

	s.admin("PATCH /v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			Role     *string `json:"role"`
			Disabled *bool   `json:"disabled"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		target, err := s.Store.GetUser(ctx, r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("user")
		}
		if err != nil {
			return err
		}
		demoting := (in.Role != nil && *in.Role != "admin") || (in.Disabled != nil && *in.Disabled)
		if target.IsAdmin() && demoting {
			n, err := s.Store.CountAdmins(ctx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return httpx.Conflict("the install needs at least one active admin")
			}
		}
		if in.Role != nil {
			if *in.Role != "admin" && *in.Role != "member" {
				return httpx.Invalid("role must be member or admin")
			}
			if err := s.Store.SetUserRole(ctx, target.ID, *in.Role); err != nil {
				return err
			}
			s.Audit(ctx, p.User.ID, "", "user.role", map[string]any{"user": target.ID, "role": *in.Role})
		}
		if in.Disabled != nil {
			if err := s.Store.SetUserDisabled(ctx, target.ID, *in.Disabled); err != nil {
				return err
			}
			if *in.Disabled {
				_ = s.Store.DeleteUserSessions(ctx, target.ID)
			}
			s.Audit(ctx, p.User.ID, "", "user.disable", map[string]any{"user": target.ID, "disabled": *in.Disabled})
		}
		u, err := s.Store.GetUser(ctx, target.ID)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"user": u})
	})

	// Without SMTP an admin hands a user this link to reset a password.
	s.admin("POST /v1/admin/users/{id}/reset-link", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		u, err := s.Store.GetUser(r.Context(), r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("user")
		}
		if err != nil {
			return err
		}
		link, err := s.CreatePasswordResetLink(r.Context(), u)
		if err != nil {
			return err
		}
		s.Audit(r.Context(), p.User.ID, "", "user.reset_link", map[string]any{"user": u.ID})
		return httpx.WriteJSON(w, 201, map[string]any{"link": link, "expiresIn": int(resetTTL.Seconds())})
	})

	s.admin("GET /v1/admin/settings", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		st, err := s.Settings(r.Context())
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"settings": st, "smtpConfigured": s.Mail.Configured()})
	})

	s.admin("PATCH /v1/admin/settings", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			AutoPauseDays      *int    `json:"autoPauseDays"`
			ResourceCap        *int    `json:"resourceCap"`
			MembersChangePlans *bool   `json:"membersChangePlans"`
			RotationGraceSecs  *int    `json:"rotationGraceSeconds"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		set := map[string]any{}
		if in.AutoPauseDays != nil {
			if *in.AutoPauseDays < 0 || *in.AutoPauseDays > 365 {
				return httpx.Invalid("autoPauseDays must be 0 (off) to 365")
			}
			set["auto_pause_days"] = *in.AutoPauseDays
		}
		if in.ResourceCap != nil {
			if *in.ResourceCap < -1 {
				return httpx.Invalid("resourceCap must be -1 (unlimited) or more")
			}
			set["resource_cap"] = *in.ResourceCap
		}
		if in.MembersChangePlans != nil {
			set["members_change_plans"] = *in.MembersChangePlans
		}
		if in.RotationGraceSecs != nil {
			if *in.RotationGraceSecs < 0 || *in.RotationGraceSecs > 7*24*3600 {
				return httpx.Invalid("rotationGraceSeconds must be 0 to 604800")
			}
			set["rotation_grace_seconds"] = *in.RotationGraceSecs
		}
		for k, v := range set {
			if err := s.Store.SetSetting(ctx, k, v); err != nil {
				return err
			}
		}
		s.Audit(ctx, p.User.ID, "", "settings.update", set)
		st, err := s.Settings(ctx)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"settings": st, "smtpConfigured": s.Mail.Configured()})
	})

	s.admin("GET /v1/admin/plans", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		plans, err := s.Store.ListPlans(r.Context(), r.URL.Query().Get("kind"), true)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"plans": plans})
	})

	s.admin("POST /v1/admin/plans", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in store.Plan
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if !planIDRe.MatchString(in.ID) {
			return httpx.Invalid("id must be lowercase letters, digits and dashes")
		}
		if _, ok := knownLimits[in.Kind]; !ok {
			return httpx.Invalid("unknown kind")
		}
		if strings.TrimSpace(in.Name) == "" {
			return httpx.Invalid("name is required")
		}
		if err := validLimits(in.Kind, in.Limits); err != nil {
			return err
		}
		plan, err := s.Store.CreatePlan(r.Context(), in)
		if errors.Is(err, store.ErrConflict) {
			return httpx.Conflict("a plan with this id exists")
		}
		if err != nil {
			return err
		}
		s.Audit(r.Context(), p.User.ID, "", "plan.create", map[string]any{"plan": plan.ID})
		return httpx.WriteJSON(w, 201, map[string]any{"plan": plan})
	})

	s.admin("PATCH /v1/admin/plans/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			Name      *string      `json:"name"`
			Limits    store.Limits `json:"limits"`
			Active    *bool        `json:"active"`
			IsDefault bool         `json:"isDefault"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		ctx := r.Context()
		plan, err := s.Store.GetPlan(ctx, r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("plan")
		}
		if err != nil {
			return err
		}
		if in.Limits != nil {
			if err := validLimits(plan.Kind, in.Limits); err != nil {
				return err
			}
		}
		if in.Active != nil && !*in.Active && plan.IsDefault {
			return httpx.Conflict("make another plan the default before deactivating this one")
		}
		if err := s.Store.UpdatePlan(ctx, plan.ID, in.Name, in.Limits, in.Active, in.IsDefault); err != nil {
			return err
		}
		s.Audit(ctx, p.User.ID, "", "plan.update", map[string]any{"plan": plan.ID})
		plan, err = s.Store.GetPlan(ctx, plan.ID)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"plan": plan})
	})

	s.admin("GET /v1/admin/hosts", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		hosts, err := s.Store.ListHosts(r.Context())
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"hosts": hosts})
	})

	s.admin("POST /v1/admin/hosts", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			Kind         string           `json:"kind"`
			Name         string           `json:"name"`
			InternalAddr string           `json:"internalAddr"`
			AdminSecret  string           `json:"adminSecret"`
			Weight       int              `json:"weight"`
			Config       store.HostConfig `json:"config"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if in.Kind != "postgres" && in.Kind != "cache" && in.Kind != "storage" {
			return httpx.Invalid("kind must be postgres, cache or storage")
		}
		if !planIDRe.MatchString(in.Name) {
			return httpx.Invalid("name must be lowercase letters, digits and dashes")
		}
		if in.Kind == "storage" {
			if in.Config.Backend != "local" || !strings.HasPrefix(in.Config.Root, "/") {
				return httpx.Invalid("storage hosts need config.backend \"local\" and an absolute config.root")
			}
			in.InternalAddr = "local"
			if in.AdminSecret == "" {
				in.AdminSecret = sbcrypto.RandomString(32)
			}
		} else if in.InternalAddr == "" {
			return httpx.Invalid("internalAddr is required, e.g. 10.0.0.5:5432")
		}
		if in.Weight == 0 {
			in.Weight = 100
		}
		h, err := s.RegisterHost(r.Context(), in.Kind, in.Name, in.InternalAddr, in.Config, in.AdminSecret, in.Weight)
		if errors.Is(err, store.ErrConflict) {
			return httpx.Conflict("a host with this name exists")
		}
		if err != nil {
			return httpx.E(422, "host_unreachable", err.Error())
		}
		s.Audit(r.Context(), p.User.ID, "", "host.add", map[string]any{"host": h.Name, "kind": h.Kind})
		h, _ = s.Store.GetHost(r.Context(), h.ID)
		return httpx.WriteJSON(w, 201, map[string]any{"host": h})
	})

	s.admin("PATCH /v1/admin/hosts/{id}", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			Status *string `json:"status"`
			Weight *int    `json:"weight"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if in.Status != nil && *in.Status != "active" && *in.Status != "draining" && *in.Status != "offline" {
			return httpx.Invalid("status must be active, draining or offline")
		}
		if in.Weight != nil && *in.Weight < 1 {
			return httpx.Invalid("weight must be at least 1")
		}
		ctx := r.Context()
		if _, err := s.Store.GetHost(ctx, r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("host")
		}
		if err := s.Store.UpdateHost(ctx, r.PathValue("id"), in.Status, in.Weight, nil); err != nil {
			return err
		}
		s.Audit(ctx, p.User.ID, "", "host.update", map[string]any{"host": r.PathValue("id")})
		h, err := s.Store.GetHost(ctx, r.PathValue("id"))
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"host": h})
	})

	s.admin("GET /v1/admin/resources", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		list, err := s.Store.ListAllResources(r.Context())
		if err != nil {
			return err
		}
		out := make([]map[string]any, 0, len(list))
		for i := range list {
			out = append(out, s.resourceJSON(&list[i]))
		}
		return httpx.OK(w, map[string]any{"resources": out})
	})

	for _, action := range []string{"suspend", "unsuspend"} {
		suspend := action == "suspend"
		s.admin("POST /v1/admin/resources/{id}/"+action, func(w http.ResponseWriter, r *http.Request, p *principal) error {
			res, err := s.Store.GetResource(r.Context(), r.PathValue("id"))
			if errors.Is(err, store.ErrNotFound) {
				return httpx.NotFound("service")
			}
			if err != nil {
				return err
			}
			if err := s.Suspend(r.Context(), p.User.ID, res, suspend); err != nil {
				return err
			}
			res, err = s.Store.GetResource(r.Context(), res.ID)
			if err != nil {
				return err
			}
			return httpx.OK(w, map[string]any{"resource": s.resourceJSON(res)})
		})
	}

	s.admin("GET /v1/admin/audit", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		list, err := s.Store.ListAudit(r.Context(), r.URL.Query().Get("resourceId"), before, queryInt(r, "limit", 100, 1, 500))
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"entries": list})
	})

	s.admin("GET /v1/admin/jobs", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		list, err := s.Store.ListJobs(r.Context(), r.URL.Query().Get("status"), queryInt(r, "limit", 100, 1, 500))
		if err != nil {
			return err
		}
		stats, err := s.Store.JobStats(r.Context())
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"jobs": list, "stats": stats})
	})

	s.user("GET /v1/status", func(w http.ResponseWriter, r *http.Request, p *principal) error {
		ctx := r.Context()
		hosts, err := s.Store.ListHosts(ctx)
		if err != nil {
			return err
		}
		stats, err := s.Store.JobStats(ctx)
		if err != nil {
			return err
		}
		type hostStatus struct {
			*store.Host
			Reachable bool   `json:"reachable"`
			Error     string `json:"error,omitempty"`
		}
		healthy := true
		var out []hostStatus
		for i := range hosts {
			h := &hosts[i]
			hs := hostStatus{Host: h, Reachable: true}
			if err := s.checkHost(ctx, h); err != nil {
				hs.Reachable, hs.Error = false, err.Error()
				if h.Status == "active" {
					healthy = false
				}
			}
			out = append(out, hs)
		}
		if !p.User.IsAdmin() {
			return httpx.OK(w, map[string]any{"healthy": healthy})
		}
		return httpx.OK(w, map[string]any{"healthy": healthy, "hosts": out, "jobs": stats})
	})
}
