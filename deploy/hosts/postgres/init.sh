#!/bin/sh
# Runs once when the tenant Postgres host is created. Adds the login
# PgBouncer uses to look up tenant passwords (auth_query). The lookup only
# returns Sparkbase tenant login roles (sbu_*), never admin roles.
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<SQL
create role sb_pgbouncer login password '${PGBOUNCER_PASSWORD}';
grant connect on database postgres to sb_pgbouncer;
create function public.sb_pgbouncer_auth(p_usename text)
  returns table (usename name, passwd text)
  language sql security definer set search_path = pg_catalog
  as \$\$ select usename, passwd from pg_catalog.pg_shadow where usename = p_usename and usename like 'sbu\_%' \$\$;
revoke all on function public.sb_pgbouncer_auth(text) from public;
grant execute on function public.sb_pgbouncer_auth(text) to sb_pgbouncer;
SQL
