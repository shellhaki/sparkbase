package drivers

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

// PGFamily provisions one database plus roles per service on a Postgres host.
// The postgres, vector, queue and auth kinds share it and differ only in the
// schema they set up.
//
// Roles per service:
//   - sbo_<id>: NOLOGIN owner of the database and every object in it.
//   - sbu_<id>_<rand>: LOGIN roles that inherit the owner and switch to it on
//     login. Rotation adds a second login role; revocation drops the old one.
type PGFamily struct {
	kind  string
	setup func(ctx context.Context, conn *pgx.Conn, t *Target) error
}

func NewPostgres() *PGFamily { return &PGFamily{kind: "postgres", setup: setupPostgres} }
func NewVector() *PGFamily   { return &PGFamily{kind: "vector", setup: setupVector} }
func NewQueue() *PGFamily    { return &PGFamily{kind: "queue", setup: setupQueue} }
func NewAuth() *PGFamily     { return &PGFamily{kind: "auth", setup: setupAuth} }

func (d *PGFamily) Kind() string     { return d.kind }
func (d *PGFamily) HostKind() string { return "postgres" }

func shortID(id string) string { return strings.ReplaceAll(id, "-", "") }

// Names derived from the immutable resource id.
func OwnerRole(r *store.Resource) string { return "sbo_" + shortID(r.ID) }
func DBName(r *store.Resource) string    { return "sb_" + shortID(r.ID) }
func loginPrefix(r *store.Resource) string {
	return "sbu_" + shortID(r.ID)[:20] + "_"
}

func qi(s string) string { return pgx.Identifier{s}.Sanitize() }

func ql(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// AdminURL builds the connection string for the host admin.
func AdminURL(h *store.Host, secret, database string) string {
	user := h.Config.AdminUser
	if user == "" {
		user = "postgres"
	}
	if database == "" {
		database = h.Config.AdminDB
		if database == "" {
			database = "postgres"
		}
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(user, secret), Host: h.InternalAddr, Path: "/" + database}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("application_name", "sparkbase-admin")
	u.RawQuery = q.Encode()
	return u.String()
}

func adminConn(ctx context.Context, t *Target, database string) (*pgx.Conn, error) {
	return pgx.Connect(ctx, AdminURL(t.H, t.AdminSecret, database))
}

// BootstrapPGHost locks a host down for tenants: no database outside a
// tenant's own is reachable by PUBLIC. It also detects pgvector.
func BootstrapPGHost(ctx context.Context, h *store.Host, secret string) (hasVector bool, err error) {
	conn, err := pgx.Connect(ctx, AdminURL(h, secret, ""))
	if err != nil {
		return false, err
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `select datname from pg_database where datname not like 'sb\_%' and datallowconn`)
	if err != nil {
		return false, err
	}
	dbs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return false, err
	}
	for _, db := range dbs {
		if _, err := conn.Exec(ctx, `revoke connect, temporary on database `+qi(db)+` from public`); err != nil {
			return false, err
		}
	}
	err = conn.QueryRow(ctx, `select exists(select 1 from pg_available_extensions where name = 'vector')`).Scan(&hasVector)
	return hasVector, err
}

func roleExists(ctx context.Context, conn *pgx.Conn, name string) (bool, error) {
	var ok bool
	err := conn.QueryRow(ctx, `select exists(select 1 from pg_roles where rolname = $1)`, name).Scan(&ok)
	return ok, err
}

func (d *PGFamily) Provision(ctx context.Context, t *Target) (*Provisioned, error) {
	conn, err := adminConn(ctx, t, "")
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	owner, db := OwnerRole(t.R), DBName(t.R)

	if err := t.Step("owner_role", func() error {
		ok, err := roleExists(ctx, conn, owner)
		if err != nil || ok {
			return err
		}
		_, err = conn.Exec(ctx, `create role `+qi(owner)+` nologin noinherit nocreatedb nocreaterole`)
		return err
	}); err != nil {
		return nil, err
	}

	login, _ := t.State["login_role"].(string)
	if login == "" {
		login = loginPrefix(t.R) + sbcrypto.RandomLower(6)
		t.State["login_role"] = login
		if err := t.Save(t.State); err != nil {
			return nil, err
		}
	}
	if err := t.Step("login_role", func() error { return d.createLogin(ctx, conn, t, login) }); err != nil {
		return nil, err
	}

	if err := t.Step("database", func() error {
		var exists bool
		if err := conn.QueryRow(ctx, `select exists(select 1 from pg_database where datname = $1)`, db).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			// CREATE DATABASE cannot run inside a transaction; pgx Exec without
			// a transaction is fine.
			if _, err := conn.Exec(ctx, `create database `+qi(db)+` owner `+qi(owner)+` template template0 encoding 'UTF8'`); err != nil {
				return err
			}
		}
		for _, stmt := range []string{
			`revoke all on database ` + qi(db) + ` from public`,
			`grant connect, temporary on database ` + qi(db) + ` to ` + qi(owner),
		} {
			if _, err := conn.Exec(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := t.Step("schema", func() error {
		tc, err := adminConn(ctx, t, db)
		if err != nil {
			return err
		}
		defer tc.Close(ctx)
		if _, err := tc.Exec(ctx, `revoke all on schema public from public; grant all on schema public to `+qi(owner)); err != nil {
			return err
		}
		return d.setup(ctx, tc, t)
	}); err != nil {
		return nil, err
	}

	// The password is reset on every attempt, so a retry after a crash never
	// leaves a credential the control plane does not know.
	password := sbcrypto.RandomString(32)
	if _, err := conn.Exec(ctx, `alter role `+qi(login)+` login password `+ql(password)); err != nil {
		return nil, err
	}
	if err := d.applyRoleLimits(ctx, conn, t, login); err != nil {
		return nil, err
	}
	return &Provisioned{ExternalName: db, Credentials: Credentials{Username: login, Secret: password}}, nil
}

func (d *PGFamily) createLogin(ctx context.Context, conn *pgx.Conn, t *Target, login string) error {
	owner := OwnerRole(t.R)
	ok, err := roleExists(ctx, conn, login)
	if err != nil {
		return err
	}
	if !ok {
		if _, err := conn.Exec(ctx, `create role `+qi(login)+` nologin inherit nocreatedb nocreaterole noreplication nobypassrls`); err != nil {
			return err
		}
	}
	for _, stmt := range []string{
		`grant ` + qi(owner) + ` to ` + qi(login),
		`alter role ` + qi(login) + ` set role = ` + ql(owner),
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (d *PGFamily) applyRoleLimits(ctx context.Context, conn *pgx.Conn, t *Target, login string) error {
	conns := t.Limits.Get("direct_connections")
	if conns <= 0 {
		conns = 5
	}
	// The API keeps up to three pooled connections per service on top of
	// the user's own direct connections.
	conns += 3
	timeout := t.Limits.Get("statement_timeout_ms")
	if timeout <= 0 {
		timeout = 30000
	}
	workMem := t.Limits.Get("work_mem_mb")
	if workMem <= 0 {
		workMem = 4
	}
	stmts := []string{
		fmt.Sprintf(`alter role %s connection limit %d`, qi(login), conns),
		fmt.Sprintf(`alter role %s set statement_timeout = %d`, qi(login), timeout),
		`alter role ` + qi(login) + ` set idle_in_transaction_session_timeout = 60000`,
		fmt.Sprintf(`alter role %s set work_mem = '%dMB'`, qi(login), workMem),
		`alter role ` + qi(login) + ` set temp_file_limit = '1GB'`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func terminate(ctx context.Context, conn *pgx.Conn, where string, arg string) error {
	_, err := conn.Exec(ctx, `select pg_terminate_backend(pid) from pg_stat_activity where `+where+` and pid <> pg_backend_pid()`, arg)
	return err
}

func (d *PGFamily) Deprovision(ctx context.Context, t *Target) error {
	conn, err := adminConn(ctx, t, "")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	db, owner := DBName(t.R), OwnerRole(t.R)
	if _, err := conn.Exec(ctx, `update pg_database set datallowconn = false where datname = $1`, db); err != nil {
		// Not fatal: only superusers may do this; terminate still works.
		_ = err
	}
	if err := terminate(ctx, conn, "datname = $1", db); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `drop database if exists `+qi(db)+` with (force)`); err != nil {
		return err
	}
	rows, err := conn.Query(ctx, `select rolname from pg_roles where rolname like $1`, loginPrefix(t.R)+"%")
	if err != nil {
		return err
	}
	logins, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, l := range append(logins, owner) {
		if _, err := conn.Exec(ctx, `drop role if exists `+qi(l)); err != nil {
			return err
		}
	}
	return nil
}

func (d *PGFamily) setLogin(ctx context.Context, t *Target, creds []Credentials, on bool) error {
	conn, err := adminConn(ctx, t, "")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	for _, c := range creds {
		verb := "nologin"
		if on {
			verb = "login"
		}
		if _, err := conn.Exec(ctx, `alter role `+qi(c.Username)+` `+verb); err != nil {
			return err
		}
		if !on {
			if err := terminate(ctx, conn, "usename = $1", c.Username); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *PGFamily) Pause(ctx context.Context, t *Target, creds []Credentials) error {
	return d.setLogin(ctx, t, creds, false)
}

func (d *PGFamily) Resume(ctx context.Context, t *Target, creds []Credentials) error {
	return d.setLogin(ctx, t, creds, true)
}

func (d *PGFamily) NewCredentials(ctx context.Context, t *Target) (*Credentials, error) {
	conn, err := adminConn(ctx, t, "")
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	login := loginPrefix(t.R) + sbcrypto.RandomLower(6)
	if err := d.createLogin(ctx, conn, t, login); err != nil {
		return nil, err
	}
	password := sbcrypto.RandomString(32)
	if _, err := conn.Exec(ctx, `alter role `+qi(login)+` login password `+ql(password)); err != nil {
		return nil, err
	}
	if err := d.applyRoleLimits(ctx, conn, t, login); err != nil {
		return nil, err
	}
	if t.R.WriteLocked {
		if _, err := conn.Exec(ctx, `alter role `+qi(login)+` set default_transaction_read_only = on`); err != nil {
			return nil, err
		}
	}
	return &Credentials{Username: login, Secret: password}, nil
}

func (d *PGFamily) RevokeCredentials(ctx context.Context, t *Target, c Credentials) error {
	conn, err := adminConn(ctx, t, "")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if ok, err := roleExists(ctx, conn, c.Username); err != nil || !ok {
		return err
	}
	if _, err := conn.Exec(ctx, `alter role `+qi(c.Username)+` nologin`); err != nil {
		return err
	}
	if err := terminate(ctx, conn, "usename = $1", c.Username); err != nil {
		return err
	}
	// Hand anything the login role created to the owner before dropping it.
	tc, err := adminConn(ctx, t, DBName(t.R))
	if err == nil {
		_, _ = tc.Exec(ctx, `reassign owned by `+qi(c.Username)+` to `+qi(OwnerRole(t.R)))
		_, _ = tc.Exec(ctx, `drop owned by `+qi(c.Username))
		tc.Close(ctx)
	}
	_, err = conn.Exec(ctx, `drop role if exists `+qi(c.Username))
	return err
}

func (d *PGFamily) ApplyLimits(ctx context.Context, t *Target, creds []Credentials) error {
	conn, err := adminConn(ctx, t, "")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	for _, c := range creds {
		if err := d.applyRoleLimits(ctx, conn, t, c.Username); err != nil {
			return err
		}
	}
	return nil
}

func (d *PGFamily) MeasureUsage(ctx context.Context, t *Target) (Usage, error) {
	conn, err := adminConn(ctx, t, "")
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	u := Usage{}
	var size, conns int64
	if err := conn.QueryRow(ctx, `select pg_database_size($1)`, DBName(t.R)).Scan(&size); err != nil {
		return nil, err
	}
	if err := conn.QueryRow(ctx, `select count(*) from pg_stat_activity where datname = $1`, DBName(t.R)).Scan(&conns); err != nil {
		return nil, err
	}
	u["storage_bytes"], u["connections"] = size, conns
	if d.kind == "auth" || d.kind == "queue" {
		tc, err := adminConn(ctx, t, DBName(t.R))
		if err != nil {
			return nil, err
		}
		defer tc.Close(ctx)
		var n int64
		if d.kind == "auth" {
			if err := tc.QueryRow(ctx, `select count(*) from sb_users`).Scan(&n); err == nil {
				u["users"] = n
			}
		} else {
			if err := tc.QueryRow(ctx, `select count(*) from sb_messages where not dead`).Scan(&n); err == nil {
				u["messages"] = n
			}
		}
	}
	return u, nil
}

func (d *PGFamily) SetWriteLock(ctx context.Context, t *Target, creds []Credentials, locked bool) error {
	conn, err := adminConn(ctx, t, "")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	for _, c := range creds {
		stmt := `alter role ` + qi(c.Username) + ` reset default_transaction_read_only`
		if locked {
			stmt = `alter role ` + qi(c.Username) + ` set default_transaction_read_only = on`
		}
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return err
		}
		// Existing sessions keep their settings; end them so the change applies.
		if err := terminate(ctx, conn, "usename = $1", c.Username); err != nil {
			return err
		}
	}
	return nil
}

// Kind-specific schema. Each runs on an admin connection to the tenant
// database and switches to the owner role so objects belong to the tenant.

func asOwner(ctx context.Context, conn *pgx.Conn, t *Target, sql string) error {
	_, err := conn.Exec(ctx, `set role `+qi(OwnerRole(t.R))+`; `+sql+`; reset role`)
	return err
}

var allowedExtensions = []string{"pgcrypto", "uuid-ossp", "pg_trgm"}

func setupPostgres(ctx context.Context, conn *pgx.Conn, t *Target) error {
	for _, ext := range allowedExtensions {
		if _, err := conn.Exec(ctx, `create extension if not exists `+qi(ext)); err != nil {
			return err
		}
	}
	if t.H.Config.HasVector {
		if _, err := conn.Exec(ctx, `create extension if not exists vector`); err != nil {
			return err
		}
	}
	return nil
}

func setupVector(ctx context.Context, conn *pgx.Conn, t *Target) error {
	if t.H.Config.HasVector {
		if _, err := conn.Exec(ctx, `create extension if not exists vector`); err != nil {
			return err
		}
	}
	return asOwner(ctx, conn, t, `
		create table if not exists sb_collections (
		  name       text primary key,
		  dimension  int not null check (dimension > 0),
		  metric     text not null check (metric in ('cosine','l2','inner_product')),
		  native     boolean not null,
		  created_at timestamptz not null default now());
		create or replace function sb_l2(a real[], b real[]) returns double precision
		  language sql immutable parallel safe
		  as $$ select sqrt(sum((x - y) * (x - y))) from unnest(a, b) as t(x, y) $$;
		create or replace function sb_neg_ip(a real[], b real[]) returns double precision
		  language sql immutable parallel safe
		  as $$ select -sum(x * y) from unnest(a, b) as t(x, y) $$;
		create or replace function sb_cosine(a real[], b real[]) returns double precision
		  language sql immutable parallel safe
		  as $$ select 1 - sum(x * y) / nullif(sqrt(sum(x * x)) * sqrt(sum(y * y)), 0) from unnest(a, b) as t(x, y) $$`)
}

func setupQueue(ctx context.Context, conn *pgx.Conn, t *Target) error {
	return asOwner(ctx, conn, t, `
		create table if not exists sb_queues (
		  name               text primary key,
		  visibility_timeout int not null default 30 check (visibility_timeout between 1 and 43200),
		  max_receives       int not null default 5 check (max_receives between 1 and 1000),
		  created_at         timestamptz not null default now());
		create table if not exists sb_messages (
		  id          bigserial primary key,
		  queue       text not null references sb_queues(name) on delete cascade,
		  body        jsonb not null,
		  enqueued_at timestamptz not null default now(),
		  visible_at  timestamptz not null default now(),
		  receives    int not null default 0,
		  dead        boolean not null default false);
		create index if not exists sb_messages_ready_idx on sb_messages (queue, visible_at, id) where not dead`)
}

func setupAuth(ctx context.Context, conn *pgx.Conn, t *Target) error {
	return asOwner(ctx, conn, t, `
		create table if not exists sb_users (
		  id                 uuid primary key default gen_random_uuid(),
		  email              text not null unique,
		  password_hash      text,
		  data               jsonb not null default '{}',
		  app_data           jsonb not null default '{}',
		  email_confirmed_at timestamptz,
		  last_sign_in_at    timestamptz,
		  banned_at          timestamptz,
		  created_at         timestamptz not null default now(),
		  updated_at         timestamptz not null default now());
		create table if not exists sb_otps (
		  id          uuid primary key default gen_random_uuid(),
		  email       text not null,
		  token_hash  text not null unique,
		  code_hash   text not null,
		  attempts    int not null default 0,
		  redirect_to text,
		  data        jsonb,
		  expires_at  timestamptz not null,
		  used_at     timestamptz,
		  created_at  timestamptz not null default now());
		create index if not exists sb_otps_email_idx on sb_otps (email, created_at desc);
		create table if not exists sb_refresh_tokens (
		  id         bigserial primary key,
		  user_id    uuid not null references sb_users(id) on delete cascade,
		  token_hash text not null unique,
		  family     uuid not null,
		  revoked    boolean not null default false,
		  expires_at timestamptz not null,
		  created_at timestamptz not null default now());
		create index if not exists sb_refresh_family_idx on sb_refresh_tokens (family);
		create table if not exists sb_settings (
		  key   text primary key,
		  value jsonb not null);
		insert into sb_settings (key, value) values
		  ('allow_signups', 'true'), ('site_url', '""'), ('redirect_urls', '[]'),
		  ('access_token_ttl', '3600'), ('refresh_token_ttl', '2592000')
		on conflict (key) do nothing`)
}
