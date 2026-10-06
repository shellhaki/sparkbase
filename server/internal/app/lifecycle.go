package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/drivers"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}[a-z0-9]$|^[a-z]$`)

// ValidName checks a service name: lowercase letters, digits and dashes.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Settings is the typed view of install settings.
type Settings struct {
	AutoPauseDays      int  `json:"autoPauseDays"`
	ResourceCap        int  `json:"resourceCap"`
	MembersChangePlans bool `json:"membersChangePlans"`
	RotationGraceSecs  int  `json:"rotationGraceSeconds"`
}

func (a *App) Settings(ctx context.Context) (Settings, error) {
	raw, err := a.Store.Settings(ctx)
	if err != nil {
		return Settings{}, err
	}
	s := Settings{ResourceCap: 10, MembersChangePlans: true, RotationGraceSecs: 3600}
	get := func(k string, v any) {
		if b, ok := raw[k]; ok {
			_ = json.Unmarshal(b, v)
		}
	}
	get("auto_pause_days", &s.AutoPauseDays)
	get("resource_cap", &s.ResourceCap)
	get("members_change_plans", &s.MembersChangePlans)
	get("rotation_grace_seconds", &s.RotationGraceSecs)
	return s, nil
}

// CreateResource validates and inserts a service, then queues provisioning.
func (a *App) CreateResource(ctx context.Context, actor *store.User, p *store.Project, kind, name, planID string) (*store.Resource, error) {
	if _, err := a.Drivers.Get(kind); err != nil {
		return nil, httpx.Invalid("kind must be one of postgres, cache, storage, vector, queue, auth")
	}
	if !ValidName(name) {
		return nil, httpx.Invalid("name must be 1 to 31 lowercase letters, digits or dashes, starting with a letter")
	}
	if p.Status != "active" {
		return nil, httpx.E(409, "conflict", "the project is "+p.Status)
	}
	var plan *store.Plan
	var err error
	if planID == "" {
		plan, err = a.Store.DefaultPlan(ctx, kind)
	} else {
		plan, err = a.Store.GetPlan(ctx, planID)
	}
	if errors.Is(err, store.ErrNotFound) || (plan != nil && (plan.Kind != kind || !plan.Active)) {
		return nil, httpx.Invalid("no active plan " + planID + " for kind " + kind)
	}
	if err != nil {
		return nil, err
	}
	if !actor.IsAdmin() {
		s, err := a.Settings(ctx)
		if err != nil {
			return nil, err
		}
		n, err := a.Store.CountUserResources(ctx, p.OwnerID)
		if err != nil {
			return nil, err
		}
		if s.ResourceCap >= 0 && n >= s.ResourceCap {
			return nil, httpx.E(403, "resource_limit_reached",
				fmt.Sprintf("you can have at most %d services on this install", s.ResourceCap)).With("limit", s.ResourceCap)
		}
	}
	if kind == "auth" {
		if _, err := a.Store.ProjectAuthResource(ctx, p.ID); err == nil {
			return nil, httpx.Conflict("a project can have only one auth service")
		}
	}
	r, err := a.Store.CreateResource(ctx, p.ID, kind, name, plan.ID)
	if errors.Is(err, store.ErrConflict) {
		return nil, httpx.Conflict("a service named " + name + " already exists in this project")
	}
	if err != nil {
		return nil, err
	}
	if err := a.Enqueue(ctx, "provision_resource", map[string]string{"resourceId": r.ID}, "provision:"+r.ID); err != nil {
		return nil, err
	}
	a.audit(ctx, actor.ID, r, "resource.create", map[string]any{"kind": kind, "name": name, "plan": plan.ID})
	return r, nil
}

func (a *App) audit(ctx context.Context, actorID string, r *store.Resource, action string, detail map[string]any) {
	if err := a.Store.Audit(ctx, store.AuditEvent{ActorID: actorID, ProjectID: r.ProjectID, ResourceID: r.ID, Action: action, Detail: detail}); err != nil {
		a.Log.Error("audit", "err", err.Error())
	}
}

// Audit writes an audit row without a resource.
func (a *App) Audit(ctx context.Context, actorID, projectID, action string, detail map[string]any) {
	if err := a.Store.Audit(ctx, store.AuditEvent{ActorID: actorID, ProjectID: projectID, Action: action, Detail: detail}); err != nil {
		a.Log.Error("audit", "err", err.Error())
	}
}

// ProvisionResource is the provision_resource job.
func (a *App) ProvisionResource(ctx context.Context, id string) error {
	r, err := a.Store.GetResource(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Status != "provisioning" {
		return nil
	}
	if r.HostID == nil {
		h, err := a.Store.PickHost(ctx, drivers.HostKindFor(r.Kind))
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no active %s host is registered", drivers.HostKindFor(r.Kind))
		}
		if err != nil {
			return err
		}
		if err := a.Store.SetResourceHost(ctx, r.ID, h.ID); err != nil {
			return err
		}
		if r, err = a.Store.GetResource(ctx, id); err != nil {
			return err
		}
	}
	t, d, err := a.Target(ctx, r)
	if err != nil {
		return err
	}
	res, err := d.Provision(ctx, t)
	if err != nil {
		return err
	}
	extra := []byte(nil)
	if r.Kind == "auth" {
		// The JWT signing secret lives next to the database credential.
		res.Credentials.Extra = sbcrypto.RandomString(48)
	}
	if res.Credentials.Extra != "" {
		extra = a.Box.SealString(res.Credentials.Extra)
	}
	endpoint := a.endpointName(r)
	err = a.Store.Tx(ctx, func(tx *store.Store) error {
		if err := deleteCredentials(ctx, tx, r.ID); err != nil {
			return err
		}
		if _, err := tx.AddCredential(ctx, r.ID, res.Credentials.Username, a.Box.SealString(res.Credentials.Secret), extra); err != nil {
			return err
		}
		if err := tx.SetResourceProvisioned(ctx, r.ID, res.ExternalName, endpoint); err != nil {
			return err
		}
		ok, err := tx.TransitionResource(ctx, r.ID, "active", "provisioning")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("service left provisioning while it was being created")
		}
		return nil
	})
	if err != nil {
		return err
	}
	a.audit(ctx, "", r, "resource.provisioned", map[string]any{"host": t.H.Name})
	return nil
}

func deleteCredentials(ctx context.Context, tx *store.Store, resourceID string) error {
	list, err := tx.ListCredentials(ctx, resourceID)
	if err != nil {
		return err
	}
	for _, c := range list {
		if err := tx.MarkCredentialRevoked(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}

// ProvisionFailed marks a service failed after its job ran out of retries.
func (a *App) ProvisionFailed(ctx context.Context, id, msg string) {
	if _, err := a.Store.TransitionResource(ctx, id, "failed", "provisioning"); err != nil {
		a.Log.Error("mark failed", "err", err.Error())
		return
	}
	_ = a.Store.SetResourceStatus(ctx, id, "failed", &msg)
	if r, err := a.Store.GetResource(ctx, id); err == nil {
		a.audit(ctx, "", r, "resource.failed", map[string]any{"error": msg})
		a.NotifyOwner(ctx, r, "critical", fmt.Sprintf("Service %s could not be created: %s", r.Name, msg))
	}
}

// RetryResource puts a failed service back into provisioning.
func (a *App) RetryResource(ctx context.Context, actor *store.User, r *store.Resource) error {
	ok, err := a.Store.TransitionResource(ctx, r.ID, "provisioning", "failed")
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Conflict("only failed services can be retried")
	}
	a.audit(ctx, actor.ID, r, "resource.retry", nil)
	return a.Enqueue(ctx, "provision_resource", map[string]string{"resourceId": r.ID}, "provision:"+r.ID)
}

// DeleteResource marks the service deleting and queues teardown.
func (a *App) DeleteResource(ctx context.Context, actorID string, r *store.Resource) error {
	if _, err := a.Store.TransitionResource(ctx, r.ID, "deleting", "provisioning", "active", "paused", "suspended", "failed"); err != nil {
		return err
	}
	a.Tenants.Invalidate(r.ID)
	a.audit(ctx, actorID, r, "resource.delete", nil)
	return a.Enqueue(ctx, "deprovision_resource", map[string]string{"resourceId": r.ID}, "deprovision:"+r.ID)
}

// DeprovisionResource is the deprovision_resource job.
func (a *App) DeprovisionResource(ctx context.Context, id string) error {
	r, err := a.Store.GetResource(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.HostID != nil {
		t, d, err := a.Target(ctx, r)
		if err != nil {
			return err
		}
		if err := d.Deprovision(ctx, t); err != nil {
			return err
		}
	}
	a.Tenants.Invalidate(r.ID)
	return a.Store.DeleteResource(ctx, r.ID)
}

// DeleteProject is the delete_project job: tear down every service, then the project.
func (a *App) DeleteProject(ctx context.Context, projectID string) error {
	list, err := a.Store.ListResources(ctx, projectID)
	if err != nil {
		return err
	}
	for _, r := range list {
		if err := a.Store.SetResourceStatus(ctx, r.ID, "deleting", nil); err != nil {
			return err
		}
		if err := a.DeprovisionResource(ctx, r.ID); err != nil {
			return err
		}
	}
	return a.Store.DeleteProject(ctx, projectID)
}

// SetPaused pauses or resumes a service right away.
func (a *App) SetPaused(ctx context.Context, actorID string, r *store.Resource, pause bool) error {
	from, to, action := "active", "paused", "resource.pause"
	if !pause {
		from, to, action = "paused", "active", "resource.resume"
	}
	if r.Status != from {
		return httpx.NotReady(r.Status)
	}
	if err := a.applyPause(ctx, r, pause); err != nil {
		return err
	}
	if ok, err := a.Store.TransitionResource(ctx, r.ID, to, from); err != nil || !ok {
		if err == nil {
			err = httpx.Conflict("the service changed state, try again")
		}
		return err
	}
	a.Tenants.Invalidate(r.ID)
	a.audit(ctx, actorID, r, action, nil)
	return nil
}

func (a *App) applyPause(ctx context.Context, r *store.Resource, pause bool) error {
	t, d, err := a.Target(ctx, r)
	if err != nil {
		return err
	}
	creds, err := a.Credentials(ctx, r.ID)
	if err != nil {
		return err
	}
	if pause {
		return d.Pause(ctx, t, creds)
	}
	return d.Resume(ctx, t, creds)
}

// Suspend blocks a service for its owner; Unsuspend restores its previous state.
func (a *App) Suspend(ctx context.Context, actorID string, r *store.Resource, suspend bool) error {
	if suspend {
		if r.Status != "active" && r.Status != "paused" {
			return httpx.NotReady(r.Status)
		}
		wasPaused := r.Status == "paused"
		if !wasPaused {
			if err := a.applyPause(ctx, r, true); err != nil {
				return err
			}
		}
		if err := a.Store.SetPausedBeforeSuspend(ctx, r.ID, wasPaused); err != nil {
			return err
		}
		if _, err := a.Store.TransitionResource(ctx, r.ID, "suspended", "active", "paused"); err != nil {
			return err
		}
		a.Tenants.Invalidate(r.ID)
		a.audit(ctx, actorID, r, "resource.suspend", nil)
		a.NotifyOwner(ctx, r, "critical", fmt.Sprintf("Service %s was suspended by an admin", r.Name))
		return nil
	}
	if r.Status != "suspended" {
		return httpx.Conflict("the service is not suspended")
	}
	to := "paused"
	if !r.PausedBeforeSuspend {
		if err := a.applyPause(ctx, r, false); err != nil {
			return err
		}
		to = "active"
	}
	if _, err := a.Store.TransitionResource(ctx, r.ID, to, "suspended"); err != nil {
		return err
	}
	a.audit(ctx, actorID, r, "resource.unsuspend", nil)
	a.NotifyOwner(ctx, r, "info", fmt.Sprintf("Service %s is no longer suspended", r.Name))
	return nil
}

// Rotate issues new credentials and schedules the old ones for revocation.
func (a *App) Rotate(ctx context.Context, actorID string, r *store.Resource) error {
	if r.Status != "active" && r.Status != "paused" {
		return httpx.NotReady(r.Status)
	}
	t, d, err := a.Target(ctx, r)
	if err != nil {
		return err
	}
	cred, err := d.NewCredentials(ctx, t)
	if err != nil {
		return err
	}
	if r.Status == "paused" {
		if err := d.Pause(ctx, t, []drivers.Credentials{*cred}); err != nil {
			return err
		}
	}
	old, err := a.Store.CurrentCredential(ctx, r.ID)
	if err != nil {
		return err
	}
	var extra []byte
	if len(old.ExtraEnc) > 0 {
		extra = old.ExtraEnc // the auth JWT secret survives credential rotation
	}
	s, err := a.Settings(ctx)
	if err != nil {
		return err
	}
	err = a.Store.Tx(ctx, func(tx *store.Store) error {
		nc, err := tx.AddCredential(ctx, r.ID, cred.Username, a.Box.SealString(cred.Secret), extra)
		if err != nil {
			return err
		}
		return tx.ScheduleRevoke(ctx, r.ID, nc.ID, a.Now().Add(time.Duration(s.RotationGraceSecs)*time.Second))
	})
	if err != nil {
		return err
	}
	a.audit(ctx, actorID, r, "resource.rotate", map[string]any{"graceSeconds": s.RotationGraceSecs})
	return nil
}

// RevokeDue drops credentials whose grace period has ended.
func (a *App) RevokeDue(ctx context.Context) error {
	due, err := a.Store.DueRevocations(ctx)
	if err != nil {
		return err
	}
	for _, c := range due {
		r, err := a.Store.GetResource(ctx, c.ResourceID)
		if err != nil {
			continue
		}
		t, d, err := a.Target(ctx, r)
		if err != nil {
			return err
		}
		cr, err := a.OpenCredential(&c)
		if err != nil {
			return err
		}
		if err := d.RevokeCredentials(ctx, t, cr); err != nil {
			return err
		}
		if err := a.Store.MarkCredentialRevoked(ctx, c.ID); err != nil {
			return err
		}
		a.Tenants.Invalidate(r.ID)
	}
	return nil
}

// ChangePlan moves a service to another plan of its kind.
func (a *App) ChangePlan(ctx context.Context, actorID string, r *store.Resource, planID string) error {
	plan, err := a.Store.GetPlan(ctx, planID)
	if errors.Is(err, store.ErrNotFound) || (plan != nil && (plan.Kind != r.Kind || !plan.Active)) {
		return httpx.Invalid("no active plan " + planID + " for kind " + r.Kind)
	}
	if err != nil {
		return err
	}
	usage, err := a.Store.LatestUsage(ctx, r.ID)
	if err != nil {
		return err
	}
	if over := OverLimits(r.Kind, usage, plan.Limits); len(over) > 0 {
		return httpx.E(409, "plan_change_blocked", "current usage is above the "+plan.Name+" plan's limits").With("limits", over)
	}
	if err := a.Store.SetResourcePlan(ctx, r.ID, plan.ID); err != nil {
		return err
	}
	r.PlanID, r.Limits = plan.ID, plan.Limits
	if r.Status == "active" || r.Status == "paused" {
		t, d, err := a.Target(ctx, r)
		if err != nil {
			return err
		}
		creds, err := a.Credentials(ctx, r.ID)
		if err != nil {
			return err
		}
		if err := d.ApplyLimits(ctx, t, creds); err != nil {
			return err
		}
	}
	a.audit(ctx, actorID, r, "resource.plan_change", map[string]any{"plan": plan.ID})
	return nil
}

// LimitPairs maps usage metrics to the plan limit that caps them, with the
// factor that converts the limit's unit into the metric's unit.
var LimitPairs = map[string][]struct {
	Metric string
	Limit  string
	Factor int64
}{
	"postgres": {{"storage_bytes", "storage_mb", 1 << 20}},
	"vector":   {{"storage_bytes", "storage_mb", 1 << 20}},
	"queue":    {{"storage_bytes", "storage_mb", 1 << 20}},
	"auth":     {{"storage_bytes", "storage_mb", 1 << 20}, {"users", "max_users", 1}},
	"cache":    {{"memory_bytes", "memory_mb", 1 << 20}},
	"storage":  {{"storage_bytes", "storage_mb", 1 << 20}},
}

// OverLimits lists the limits that usage exceeds.
func OverLimits(kind string, usage map[string]int64, limits store.Limits) []string {
	var over []string
	for _, p := range LimitPairs[kind] {
		lim := limits.Get(p.Limit)
		if lim < 0 {
			continue
		}
		if usage[p.Metric] > lim*p.Factor {
			over = append(over, p.Limit)
		}
	}
	return over
}

// endpointName is the stable native hostname used by Traefik routing.
func (a *App) endpointName(r *store.Resource) string {
	switch r.Kind {
	case "postgres", "vector":
		return r.Name + "-" + r.ProjectSlug + ".db." + a.Cfg.Domain
	case "cache":
		return r.Name + "-" + r.ProjectSlug + ".cache." + a.Cfg.Domain
	}
	return ""
}

// NativeAddr returns the host and port a native client connects to, and
// whether TLS is used.
func (a *App) NativeAddr(r *store.Resource, h *store.Host, pooled bool) (host string, port int, tls bool) {
	if a.Cfg.NativeMode == "traefik" {
		switch r.Kind {
		case "cache":
			return a.endpointName(r), a.Cfg.NativeRedisPort, true
		default:
			if pooled {
				return a.endpointName(r), a.Cfg.NativePGPooledPort, true
			}
			return a.endpointName(r), a.Cfg.NativePGPort, true
		}
	}
	host, portStr, err := net.SplitHostPort(h.InternalAddr)
	if err != nil {
		host, portStr = h.InternalAddr, "5432"
	}
	port, _ = strconv.Atoi(portStr)
	if h.Config.PublicHost != "" {
		host = h.Config.PublicHost
	}
	if h.Config.PublicPort != 0 {
		port = h.Config.PublicPort
	}
	if pooled && h.Config.PoolerPort != 0 {
		port = h.Config.PoolerPort
	}
	return host, port, false
}
