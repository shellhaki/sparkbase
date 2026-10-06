create extension if not exists pgcrypto;

create table subscribers (
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

create table rate_limits (
  ip_hash      text not null,
  window_start timestamptz not null,
  count        int not null default 1,
  primary key (ip_hash, window_start)
);
