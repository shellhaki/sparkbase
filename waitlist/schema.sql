-- Full schema for a fresh production database. Run once:
--   psql "$DATABASE_URL" -f schema.sql
-- Safe to run again: every statement skips what already exists.
-- It also records 0001_init.sql in schema_migrations, so `node scripts/migrate.mjs`
-- (the Coolify pre-deployment command) will not try to apply it a second time.

begin;

create extension if not exists pgcrypto;

create table if not exists subscribers (
  id                uuid primary key default gen_random_uuid(),
  email             text not null,
  list              text not null check (list in ('waitlist', 'newsletter')),
  source            text,
  ip_hash           text,
  unsubscribe_token uuid not null default gen_random_uuid(),
  unsubscribed_at   timestamptz,
  created_at        timestamptz not null default now(),
  unique (email, list)
);

create table if not exists rate_limits (
  ip_hash      text not null,
  window_start timestamptz not null,
  count        int not null default 1,
  primary key (ip_hash, window_start)
);

create table if not exists schema_migrations (
  name       text primary key,
  applied_at timestamptz not null default now()
);

insert into schema_migrations (name) values ('0001_init.sql')
on conflict (name) do nothing;

commit;
