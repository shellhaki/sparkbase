create extension if not exists pgcrypto;

create table users (
  id                uuid primary key default gen_random_uuid(),
  email             text not null unique,
  password_hash     text not null,            -- argon2id
  name              text,
  role              text not null default 'member' check (role in ('admin','member')),
  failed_logins     int not null default 0,
  locked_until      timestamptz,
  last_login_at     timestamptz,
  disabled_at       timestamptz,
  created_at        timestamptz not null default now()
);

create table sessions (
  id         uuid primary key default gen_random_uuid(),
  user_id    uuid not null references users(id) on delete cascade,
  token_hash text not null unique,
  expires_at timestamptz not null,
  created_at timestamptz not null default now()
);
create index sessions_user_idx on sessions (user_id);

-- One-time tokens: first-run setup, password resets and invites.
create table auth_tokens (
  id         uuid primary key default gen_random_uuid(),
  kind       text not null check (kind in ('setup','password_reset','invite')),
  user_id    uuid references users(id) on delete cascade,
  email      text,
  token_hash text not null unique,
  role       text check (role in ('admin','member')),
  invited_by uuid references users(id) on delete set null,
  expires_at timestamptz not null,
  used_at    timestamptz,
  created_at timestamptz not null default now()
);

create table settings (
  key        text primary key,
  value      jsonb not null,
  updated_at timestamptz not null default now()
);

create table projects (
  id         uuid primary key default gen_random_uuid(),
  owner_id   uuid not null references users(id) on delete cascade,
  slug       text not null unique,
  name       text not null,
  status     text not null default 'active' check (status in ('active','suspended','deleting')),
  created_at timestamptz not null default now()
);
create index projects_owner_idx on projects (owner_id);

create table project_keys (
  id         uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  kind       text not null check (kind in ('publishable','secret')),
  prefix     text not null,
  key_hash   text not null unique,
  key_enc    bytea not null,
  created_at timestamptz not null default now(),
  revoked_at timestamptz
);
create index project_keys_project_idx on project_keys (project_id);

create table hosts (
  id               uuid primary key default gen_random_uuid(),
  kind             text not null check (kind in ('postgres','cache','storage')),
  name             text not null unique,
  internal_addr    text not null,
  config           jsonb not null default '{}',
  admin_secret_enc bytea not null,
  weight           int not null default 100 check (weight > 0),
  status           text not null default 'active' check (status in ('active','draining','offline')),
  bootstrapped_at  timestamptz,
  created_at       timestamptz not null default now()
);

create table plans (
  id         text primary key,
  kind       text not null check (kind in ('postgres','cache','storage','vector','queue','auth')),
  name       text not null,
  limits     jsonb not null,
  is_default boolean not null default false,
  active     boolean not null default true,
  sort       int not null default 0
);
create unique index plans_one_default_per_kind on plans (kind) where is_default;

create table resources (
  id              uuid primary key default gen_random_uuid(),
  project_id      uuid not null references projects(id) on delete cascade,
  kind            text not null check (kind in ('postgres','cache','storage','vector','queue','auth')),
  name            text not null,
  plan_id         text not null references plans(id),
  host_id         uuid references hosts(id),
  status          text not null default 'provisioning'
                  check (status in ('provisioning','active','paused','suspended','failed','deleting')),
  paused_before_suspend boolean not null default false,
  external_name   text,
  endpoint        text,
  provision_state jsonb not null default '{}',
  write_locked    boolean not null default false,
  last_error      text,
  last_active_at  timestamptz,
  created_at      timestamptz not null default now(),
  unique (project_id, name)
);
create index resources_host_idx on resources (host_id);
create unique index resources_one_auth_per_project on resources (project_id) where kind = 'auth';

create table resource_credentials (
  id          uuid primary key default gen_random_uuid(),
  resource_id uuid not null references resources(id) on delete cascade,
  username    text not null,
  secret_enc  bytea not null,
  extra_enc   bytea,
  created_at  timestamptz not null default now(),
  revoke_at   timestamptz,
  revoked_at  timestamptz
);
create index resource_credentials_resource_idx on resource_credentials (resource_id);

create table api_keys (
  id           uuid primary key default gen_random_uuid(),
  user_id      uuid not null references users(id) on delete cascade,
  project_id   uuid references projects(id) on delete cascade,
  name         text not null,
  prefix       text not null,
  key_hash     text not null unique,
  scopes       text[] not null default '{}',
  last_used_at timestamptz,
  expires_at   timestamptz,
  revoked_at   timestamptz,
  created_at   timestamptz not null default now()
);
create index api_keys_user_idx on api_keys (user_id);

create table usage_samples (
  resource_id uuid not null references resources(id) on delete cascade,
  metric      text not null,
  sampled_at  timestamptz not null,
  value       bigint not null,
  primary key (resource_id, metric, sampled_at)
);

create table backups (
  id          uuid primary key default gen_random_uuid(),
  resource_id uuid not null references resources(id) on delete cascade,
  status      text not null default 'running' check (status in ('running','done','failed')),
  object_key  text,
  size_bytes  bigint,
  checksum    text,
  expires_at  timestamptz,
  error       text,
  created_at  timestamptz not null default now(),
  finished_at timestamptz
);
create index backups_resource_idx on backups (resource_id, created_at desc);

create table jobs (
  id           bigserial primary key,
  type         text not null,
  payload      jsonb not null default '{}',
  status       text not null default 'queued' check (status in ('queued','running','done','failed')),
  dedupe_key   text,
  run_at       timestamptz not null default now(),
  attempts     int not null default 0,
  max_attempts int not null default 5,
  locked_at    timestamptz,
  locked_by    text,
  last_error   text,
  finished_at  timestamptz,
  created_at   timestamptz not null default now()
);
create index jobs_ready_idx on jobs (run_at) where status = 'queued';
create unique index jobs_dedupe_idx on jobs (dedupe_key) where status in ('queued','running');

create table audit_log (
  id          bigserial primary key,
  actor_id    uuid,
  project_id  uuid,
  resource_id uuid,
  action      text not null,
  detail      jsonb,
  created_at  timestamptz not null default now()
);
create index audit_log_created_idx on audit_log (created_at desc);
create index audit_log_resource_idx on audit_log (resource_id, created_at desc);

create table notifications (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users(id) on delete cascade,
  resource_id uuid references resources(id) on delete cascade,
  level       text not null default 'info' check (level in ('info','warning','critical')),
  message     text not null,
  read_at     timestamptz,
  created_at  timestamptz not null default now()
);
create index notifications_user_idx on notifications (user_id, created_at desc);
