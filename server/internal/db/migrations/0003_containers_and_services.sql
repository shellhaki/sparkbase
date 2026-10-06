-- Containers with scale to zero, and the control-plane services:
-- realtime, cron, backup and monitor.

alter table resources drop constraint resources_kind_check;
alter table resources add constraint resources_kind_check
  check (kind in ('postgres','cache','storage','vector','queue','auth','realtime','cron','backup','monitor'));
alter table plans drop constraint plans_kind_check;
alter table plans add constraint plans_kind_check
  check (kind in ('postgres','cache','storage','vector','queue','auth','realtime','cron','backup','monitor'));

-- 'shared' services live on a shared data host; 'container' services run in
-- their own container, started on demand and stopped when idle.
alter table resources add column runtime text not null default 'shared' check (runtime in ('shared','container','control'));
alter table resources add column container_name text;
alter table resources add column admin_secret_enc bytea;
alter table resources add column proxy_port int unique;
-- Per-service settings for control-plane services (schedules, targets...).
alter table resources add column settings jsonb not null default '{}';

-- Memory limits for services that run in containers.
update plans set limits = limits || '{"memory_mb":256}'  where id in ('postgres-small','vector-small','queue-small','auth-small');
update plans set limits = limits || '{"memory_mb":1024}' where id in ('postgres-medium','vector-medium','queue-medium','auth-medium');
update plans set limits = limits || '{"memory_mb":4096}' where id in ('postgres-large','vector-large','queue-large','auth-large');

insert into plans (id, kind, name, limits, is_default, sort) values
  ('realtime-small',  'realtime', 'Small',  '{"max_connections":200,"messages_per_month":1000000,"requests_per_month":100000}', true, 1),
  ('realtime-medium', 'realtime', 'Medium', '{"max_connections":2000,"messages_per_month":10000000,"requests_per_month":1000000}', false, 2),
  ('realtime-large',  'realtime', 'Large',  '{"max_connections":20000,"messages_per_month":-1,"requests_per_month":-1}', false, 3),
  ('cron-small',  'cron', 'Small',  '{"max_jobs":10,"min_interval_seconds":60,"requests_per_month":100000}', true, 1),
  ('cron-medium', 'cron', 'Medium', '{"max_jobs":100,"min_interval_seconds":60,"requests_per_month":1000000}', false, 2),
  ('cron-large',  'cron', 'Large',  '{"max_jobs":1000,"min_interval_seconds":60,"requests_per_month":-1}', false, 3),
  ('backup-small',  'backup', 'Small',  '{"retention_days":7,"max_targets":5,"requests_per_month":100000}', true, 1),
  ('backup-medium', 'backup', 'Medium', '{"retention_days":30,"max_targets":25,"requests_per_month":1000000}', false, 2),
  ('backup-large',  'backup', 'Large',  '{"retention_days":90,"max_targets":100,"requests_per_month":-1}', false, 3),
  ('monitor-small',  'monitor', 'Small',  '{"max_checks":5,"min_interval_seconds":60,"requests_per_month":100000}', true, 1),
  ('monitor-medium', 'monitor', 'Medium', '{"max_checks":50,"min_interval_seconds":30,"requests_per_month":1000000}', false, 2),
  ('monitor-large',  'monitor', 'Large',  '{"max_checks":500,"min_interval_seconds":10,"requests_per_month":-1}', false, 3)
on conflict (id) do nothing;

insert into settings (key, value) values ('idle_minutes', '10') on conflict (key) do nothing;

create table cron_jobs (
  id          uuid primary key default gen_random_uuid(),
  resource_id uuid not null references resources(id) on delete cascade,
  name        text not null,
  schedule    text not null,          -- 5-field cron expression, UTC
  action      jsonb not null,         -- {type: http|sql|queue, ...}
  enabled     boolean not null default true,
  next_run_at timestamptz not null,
  last_run_at timestamptz,
  last_status text,
  created_at  timestamptz not null default now(),
  unique (resource_id, name)
);
create index cron_jobs_due_idx on cron_jobs (next_run_at) where enabled;

create table cron_runs (
  id          bigserial primary key,
  job_id      uuid not null references cron_jobs(id) on delete cascade,
  started_at  timestamptz not null default now(),
  finished_at timestamptz,
  status      text not null default 'running' check (status in ('running','ok','failed')),
  output      text
);
create index cron_runs_job_idx on cron_runs (job_id, started_at desc);

create table monitors (
  id               uuid primary key default gen_random_uuid(),
  resource_id      uuid not null references resources(id) on delete cascade,
  name             text not null,
  url              text not null,
  method           text not null default 'GET',
  interval_seconds int not null default 60,
  timeout_seconds  int not null default 10,
  expect_status    int not null default 200,
  enabled          boolean not null default true,
  state            text not null default 'unknown' check (state in ('unknown','up','down')),
  next_check_at    timestamptz not null default now(),
  created_at       timestamptz not null default now(),
  unique (resource_id, name)
);
create index monitors_due_idx on monitors (next_check_at) where enabled;

create table monitor_results (
  monitor_id uuid not null references monitors(id) on delete cascade,
  checked_at timestamptz not null,
  ok         boolean not null,
  status     int,
  latency_ms int,
  error      text,
  primary key (monitor_id, checked_at)
);

alter table backups add column destination text not null default 'local';
alter table backups add column via_resource_id uuid references resources(id) on delete set null;
