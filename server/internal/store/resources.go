package store

import (
	"context"
	"encoding/json"
	"time"
)

// Hosts

const hostCols = `h.id, h.kind, h.name, h.internal_addr, h.config, h.admin_secret_enc, h.weight, h.status, h.bootstrapped_at, h.created_at,
	(select count(*) from resources r where r.host_id = h.id and r.status <> 'deleting')::int as resource_count`

func (s *Store) CreateHost(ctx context.Context, kind, name, addr string, cfg HostConfig, secretEnc []byte, weight int) (*Host, error) {
	b, _ := json.Marshal(cfg)
	var id string
	err := s.q.QueryRow(ctx, `
		insert into hosts (kind, name, internal_addr, config, admin_secret_enc, weight)
		values ($1, $2, $3, $4, $5, $6) returning id`, kind, name, addr, b, secretEnc, weight).Scan(&id)
	if IsUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.GetHost(ctx, id)
}

func (s *Store) GetHost(ctx context.Context, id string) (*Host, error) {
	return one[Host](ctx, s.q, `select `+hostCols+` from hosts h where h.id = $1`, id)
}

func (s *Store) GetHostByName(ctx context.Context, name string) (*Host, error) {
	return one[Host](ctx, s.q, `select `+hostCols+` from hosts h where h.name = $1`, name)
}

func (s *Store) ListHosts(ctx context.Context) ([]Host, error) {
	return many[Host](ctx, s.q, `select `+hostCols+` from hosts h order by h.kind, h.name`)
}

func (s *Store) UpdateHost(ctx context.Context, id string, status *string, weight *int, cfg *HostConfig) error {
	var cfgJSON []byte
	if cfg != nil {
		cfgJSON, _ = json.Marshal(cfg)
	}
	_, err := exec(ctx, s.q, `
		update hosts set status = coalesce($2, status), weight = coalesce($3, weight), config = coalesce($4, config)
		where id = $1`, id, status, weight, cfgJSON)
	return err
}

func (s *Store) MarkHostBootstrapped(ctx context.Context, id string) error {
	_, err := exec(ctx, s.q, `update hosts set bootstrapped_at = now() where id = $1`, id)
	return err
}

// PickHost returns the active host of a kind with the lowest load relative to
// its weight. Ties go to the oldest host.
func (s *Store) PickHost(ctx context.Context, kind string) (*Host, error) {
	return one[Host](ctx, s.q, `
		select `+hostCols+` from hosts h
		where h.kind = $1 and h.status = 'active'
		order by (select count(*) from resources r where r.host_id = h.id and r.status <> 'deleting')::float / h.weight,
		         h.created_at
		limit 1`, kind)
}

// Plans

const planCols = `id, kind, name, limits, is_default, active, sort`

func (s *Store) ListPlans(ctx context.Context, kind string, includeInactive bool) ([]Plan, error) {
	return many[Plan](ctx, s.q, `
		select `+planCols+` from plans
		where ($1 = '' or kind = $1) and ($2 or active)
		order by kind, sort, id`, kind, includeInactive)
}

func (s *Store) GetPlan(ctx context.Context, id string) (*Plan, error) {
	return one[Plan](ctx, s.q, `select `+planCols+` from plans where id = $1`, id)
}

func (s *Store) DefaultPlan(ctx context.Context, kind string) (*Plan, error) {
	return one[Plan](ctx, s.q, `select `+planCols+` from plans where kind = $1 and is_default and active`, kind)
}

func (s *Store) CreatePlan(ctx context.Context, p Plan) (*Plan, error) {
	out, err := one[Plan](ctx, s.q, `
		insert into plans (id, kind, name, limits, is_default, active, sort)
		values ($1, $2, $3, $4, false, true, coalesce((select max(sort) + 1 from plans where kind = $2), 1))
		returning `+planCols, p.ID, p.Kind, p.Name, p.Limits)
	if IsUniqueViolation(err) {
		return nil, ErrConflict
	}
	return out, err
}

func (s *Store) UpdatePlan(ctx context.Context, id string, name *string, limits Limits, active *bool, makeDefault bool) error {
	return s.Tx(ctx, func(tx *Store) error {
		var limitsJSON []byte
		if limits != nil {
			limitsJSON, _ = json.Marshal(limits)
		}
		if makeDefault {
			if _, err := exec(ctx, tx.q, `update plans set is_default = false where kind = (select kind from plans where id = $1)`, id); err != nil {
				return err
			}
		}
		_, err := exec(ctx, tx.q, `
			update plans set name = coalesce($2, name), limits = coalesce($3, limits),
			  active = coalesce($4, active), is_default = is_default or $5
			where id = $1`, id, name, limitsJSON, active, makeDefault)
		return err
	})
}

// Resources

const resourceCols = `r.id, r.project_id, r.kind, r.name, r.plan_id, r.host_id, r.status, r.paused_before_suspend,
	r.runtime, r.container_name, r.admin_secret_enc, r.proxy_port, r.settings,
	r.external_name, r.endpoint, r.provision_state, r.write_locked, r.last_error, r.last_active_at, r.created_at,
	p.slug as project_slug, p.owner_id, pl.limits`

const resourceFrom = ` from resources r join projects p on p.id = r.project_id join plans pl on pl.id = r.plan_id `

func (s *Store) CreateResource(ctx context.Context, projectID, kind, name, planID string) (*Resource, error) {
	var id string
	err := s.q.QueryRow(ctx, `
		insert into resources (project_id, kind, name, plan_id) values ($1, $2, $3, $4) returning id`,
		projectID, kind, name, planID).Scan(&id)
	if IsUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.GetResource(ctx, id)
}

func (s *Store) GetResource(ctx context.Context, id string) (*Resource, error) {
	return one[Resource](ctx, s.q, `select `+resourceCols+resourceFrom+`where r.id = $1`, id)
}

func (s *Store) GetResourceByName(ctx context.Context, projectID, kind, name string) (*Resource, error) {
	return one[Resource](ctx, s.q, `select `+resourceCols+resourceFrom+`where r.project_id = $1 and r.kind = $2 and r.name = $3`, projectID, kind, name)
}

func (s *Store) ProjectAuthResource(ctx context.Context, projectID string) (*Resource, error) {
	return one[Resource](ctx, s.q, `select `+resourceCols+resourceFrom+`where r.project_id = $1 and r.kind = 'auth'`, projectID)
}

func (s *Store) ListResources(ctx context.Context, projectID string) ([]Resource, error) {
	return many[Resource](ctx, s.q, `select `+resourceCols+resourceFrom+`where r.project_id = $1 order by r.created_at`, projectID)
}

// ListResourcesByStatus returns resources in any of the given statuses across the install.
func (s *Store) ListResourcesByStatus(ctx context.Context, statuses ...string) ([]Resource, error) {
	return many[Resource](ctx, s.q, `select `+resourceCols+resourceFrom+`where r.status = any($1) order by r.created_at`, statuses)
}

func (s *Store) ListAllResources(ctx context.Context) ([]Resource, error) {
	return many[Resource](ctx, s.q, `select `+resourceCols+resourceFrom+`order by r.created_at desc`)
}

func (s *Store) CountUserResources(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `
		select count(*) from resources r join projects p on p.id = r.project_id
		where p.owner_id = $1 and r.status <> 'deleting'`, userID).Scan(&n)
	return n, err
}

func (s *Store) SetResourceStatus(ctx context.Context, id, status string, lastError *string) error {
	_, err := exec(ctx, s.q, `update resources set status = $2, last_error = $3 where id = $1`, id, status, lastError)
	return err
}

// TransitionResource changes status only when the current status is one of from.
func (s *Store) TransitionResource(ctx context.Context, id, to string, from ...string) (bool, error) {
	n, err := exec(ctx, s.q, `update resources set status = $2, last_error = null where id = $1 and status = any($3)`, id, to, from)
	return n > 0, err
}

func (s *Store) SetResourceHost(ctx context.Context, id, hostID string) error {
	_, err := exec(ctx, s.q, `update resources set host_id = $2 where id = $1 and host_id is null`, id, hostID)
	return err
}

func (s *Store) SetResourceProvisioned(ctx context.Context, id, externalName, endpoint string) error {
	_, err := exec(ctx, s.q, `update resources set external_name = $2, endpoint = $3 where id = $1`, id, externalName, endpoint)
	return err
}

func (s *Store) SetProvisionState(ctx context.Context, id string, state map[string]any) error {
	b, _ := json.Marshal(state)
	_, err := exec(ctx, s.q, `update resources set provision_state = $2 where id = $1`, id, b)
	return err
}

func (s *Store) SetResourceName(ctx context.Context, id, name string) error {
	_, err := exec(ctx, s.q, `update resources set name = $2 where id = $1`, id, name)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) SetResourcePlan(ctx context.Context, id, planID string) error {
	_, err := exec(ctx, s.q, `update resources set plan_id = $2 where id = $1`, id, planID)
	return err
}

func (s *Store) SetWriteLocked(ctx context.Context, id string, locked bool) error {
	_, err := exec(ctx, s.q, `update resources set write_locked = $2 where id = $1`, id, locked)
	return err
}

func (s *Store) SetPausedBeforeSuspend(ctx context.Context, id string, v bool) error {
	_, err := exec(ctx, s.q, `update resources set paused_before_suspend = $2 where id = $1`, id, v)
	return err
}

func (s *Store) TouchResource(ctx context.Context, id string) error {
	_, err := exec(ctx, s.q, `update resources set last_active_at = now() where id = $1 and (last_active_at is null or last_active_at < now() - interval '1 minute')`, id)
	return err
}

func (s *Store) DeleteResource(ctx context.Context, id string) error {
	_, err := exec(ctx, s.q, `delete from resources where id = $1`, id)
	return err
}

// Credentials

func (s *Store) AddCredential(ctx context.Context, resourceID, username string, secretEnc, extraEnc []byte) (*Credential, error) {
	return one[Credential](ctx, s.q, `
		insert into resource_credentials (resource_id, username, secret_enc, extra_enc)
		values ($1, $2, $3, $4)
		returning id, resource_id, username, secret_enc, extra_enc, created_at, revoke_at, revoked_at`,
		resourceID, username, secretEnc, extraEnc)
}

// CurrentCredential is the newest credential that is not scheduled for revocation.
func (s *Store) CurrentCredential(ctx context.Context, resourceID string) (*Credential, error) {
	return one[Credential](ctx, s.q, `
		select id, resource_id, username, secret_enc, extra_enc, created_at, revoke_at, revoked_at
		from resource_credentials
		where resource_id = $1 and revoked_at is null and revoke_at is null
		order by created_at desc limit 1`, resourceID)
}

func (s *Store) ListCredentials(ctx context.Context, resourceID string) ([]Credential, error) {
	return many[Credential](ctx, s.q, `
		select id, resource_id, username, secret_enc, extra_enc, created_at, revoke_at, revoked_at
		from resource_credentials where resource_id = $1 and revoked_at is null order by created_at`, resourceID)
}

// ScheduleRevoke marks every current credential except keepID to be revoked at the given time.
func (s *Store) ScheduleRevoke(ctx context.Context, resourceID, keepID string, at time.Time) error {
	_, err := exec(ctx, s.q, `
		update resource_credentials set revoke_at = $3
		where resource_id = $1 and id <> $2 and revoked_at is null and revoke_at is null`, resourceID, keepID, at)
	return err
}

// DueRevocations returns credentials whose grace period has ended.
func (s *Store) DueRevocations(ctx context.Context) ([]Credential, error) {
	return many[Credential](ctx, s.q, `
		select id, resource_id, username, secret_enc, extra_enc, created_at, revoke_at, revoked_at
		from resource_credentials where revoked_at is null and revoke_at <= now()`)
}

func (s *Store) MarkCredentialRevoked(ctx context.Context, id string) error {
	_, err := exec(ctx, s.q, `update resource_credentials set revoked_at = now() where id = $1`, id)
	return err
}

// SetContainer records the container a service runs in.
func (s *Store) SetContainer(ctx context.Context, id, name string, adminSecretEnc []byte, port int) error {
	_, err := exec(ctx, s.q, `
		update resources set runtime = 'container', container_name = $2, admin_secret_enc = $3, proxy_port = $4
		where id = $1`, id, name, adminSecretEnc, port)
	return err
}

func (s *Store) SetRuntime(ctx context.Context, id, runtime string) error {
	_, err := exec(ctx, s.q, `update resources set runtime = $2 where id = $1`, id, runtime)
	return err
}

// NextProxyPort hands out an unused internal port for the activator.
func (s *Store) NextProxyPort(ctx context.Context, base int) (int, error) {
	var p int
	err := s.q.QueryRow(ctx, `
		select coalesce(max(proxy_port) + 1, $1) from resources where proxy_port is not null`, base).Scan(&p)
	return p, err
}

func (s *Store) SetResourceSettings(ctx context.Context, id string, settings any) error {
	b, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = exec(ctx, s.q, `update resources set settings = $2 where id = $1`, id, b)
	return err
}

// ContainerResources returns services that run in containers.
func (s *Store) ContainerResources(ctx context.Context) ([]Resource, error) {
	return many[Resource](ctx, s.q, `select `+resourceCols+resourceFrom+`where r.runtime = 'container' and r.container_name is not null order by r.created_at`)
}
