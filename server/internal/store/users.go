package store

import (
	"context"
	"encoding/json"
	"time"
)

const userCols = `id, email, password_hash, name, role, failed_logins, locked_until, last_login_at, disabled_at, created_at`

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `select count(*) from users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string, name *string, role string) (*User, error) {
	u, err := one[User](ctx, s.q, `
		insert into users (email, password_hash, name, role) values (lower($1), $2, $3, $4)
		returning `+userCols, email, passwordHash, name, role)
	if IsUniqueViolation(err) {
		return nil, ErrConflict
	}
	return u, err
}

func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	return one[User](ctx, s.q, `select `+userCols+` from users where id = $1`, id)
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return one[User](ctx, s.q, `select `+userCols+` from users where email = lower($1)`, email)
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	return many[User](ctx, s.q, `select `+userCols+` from users order by created_at`)
}

func (s *Store) UpdateUserProfile(ctx context.Context, id string, name *string) error {
	_, err := exec(ctx, s.q, `update users set name = $2 where id = $1`, id, name)
	return err
}

func (s *Store) SetUserRole(ctx context.Context, id, role string) error {
	_, err := exec(ctx, s.q, `update users set role = $2 where id = $1`, id, role)
	return err
}

func (s *Store) SetUserDisabled(ctx context.Context, id string, disabled bool) error {
	_, err := exec(ctx, s.q, `update users set disabled_at = case when $2 then now() end where id = $1`, id, disabled)
	return err
}

func (s *Store) TouchLogin(ctx context.Context, id string) error {
	_, err := exec(ctx, s.q, `update users set last_login_at = now(), failed_logins = 0, locked_until = null where id = $1`, id)
	return err
}

func (s *Store) SetUserPassword(ctx context.Context, id, hash string) error {
	_, err := exec(ctx, s.q, `update users set password_hash = $2, failed_logins = 0, locked_until = null where id = $1`, id, hash)
	return err
}

// RecordFailedLogin counts a wrong password and locks the account for
// lockFor once the count reaches max.
func (s *Store) RecordFailedLogin(ctx context.Context, id string, max int, lockFor time.Duration) error {
	_, err := exec(ctx, s.q, `
		update users set failed_logins = failed_logins + 1,
		  locked_until = case when failed_logins + 1 >= $2 then now() + make_interval(secs => $3) else locked_until end
		where id = $1`, id, max, lockFor.Seconds())
	return err
}

// ConsumeUserTokens marks every unused token of a kind for a user as used.
func (s *Store) ConsumeUserTokens(ctx context.Context, userID, kind string) error {
	_, err := exec(ctx, s.q, `update auth_tokens set used_at = now() where user_id = $1 and kind = $2 and used_at is null`, userID, kind)
	return err
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `select count(*) from users where role = 'admin' and disabled_at is null`).Scan(&n)
	return n, err
}

// Sessions

func (s *Store) CreateSession(ctx context.Context, userID, tokenHash string, expires time.Time) error {
	_, err := exec(ctx, s.q, `insert into sessions (user_id, token_hash, expires_at) values ($1, $2, $3)`, userID, tokenHash, expires)
	return err
}

func (s *Store) UserBySession(ctx context.Context, tokenHash string) (*User, error) {
	return one[User](ctx, s.q, `
		select u.id, u.email, u.password_hash, u.name, u.role, u.failed_logins, u.locked_until,
		       u.last_login_at, u.disabled_at, u.created_at
		from sessions s join users u on u.id = s.user_id
		where s.token_hash = $1 and s.expires_at > now()`, tokenHash)
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := exec(ctx, s.q, `delete from sessions where token_hash = $1`, tokenHash)
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := exec(ctx, s.q, `delete from sessions where user_id = $1`, userID)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := exec(ctx, s.q, `delete from sessions where expires_at < now()`)
	return err
}

// Auth tokens: setup, password_reset, invite.

const tokenCols = `id, kind, user_id, email, role, invited_by, expires_at, used_at, created_at`

type NewAuthToken struct {
	Kind      string
	UserID    *string
	Email     *string
	TokenHash string
	Role      *string
	InvitedBy *string
	ExpiresAt time.Time
}

func (s *Store) CreateAuthToken(ctx context.Context, t NewAuthToken) (*AuthToken, error) {
	return one[AuthToken](ctx, s.q, `
		insert into auth_tokens (kind, user_id, email, token_hash, role, invited_by, expires_at)
		values ($1, $2, lower($3), $4, $5, $6, $7) returning `+tokenCols,
		t.Kind, t.UserID, t.Email, t.TokenHash, t.Role, t.InvitedBy, t.ExpiresAt)
}

// ValidAuthToken returns an unused, unexpired token without consuming it.
func (s *Store) ValidAuthToken(ctx context.Context, kind, tokenHash string) (*AuthToken, error) {
	return one[AuthToken](ctx, s.q, `
		select `+tokenCols+` from auth_tokens
		where kind = $1 and token_hash = $2 and used_at is null and expires_at > now()`, kind, tokenHash)
}

// ConsumeAuthToken marks a valid token used and returns it; used tokens and
// expired tokens return ErrNotFound.
func (s *Store) ConsumeAuthToken(ctx context.Context, kind, tokenHash string) (*AuthToken, error) {
	return one[AuthToken](ctx, s.q, `
		update auth_tokens set used_at = now()
		where kind = $1 and token_hash = $2 and used_at is null and expires_at > now()
		returning `+tokenCols, kind, tokenHash)
}

func (s *Store) DeleteAuthTokens(ctx context.Context, kind string) error {
	_, err := exec(ctx, s.q, `delete from auth_tokens where kind = $1`, kind)
	return err
}

func (s *Store) ListPendingInvites(ctx context.Context) ([]AuthToken, error) {
	return many[AuthToken](ctx, s.q, `
		select `+tokenCols+` from auth_tokens
		where kind = 'invite' and used_at is null and expires_at > now()
		order by created_at desc`)
}

func (s *Store) DeleteInvite(ctx context.Context, id string) (bool, error) {
	n, err := exec(ctx, s.q, `delete from auth_tokens where id = $1 and kind = 'invite' and used_at is null`, id)
	return n > 0, err
}

// Settings

func (s *Store) Settings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.q.Query(ctx, `select key, value from settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SetSetting(ctx context.Context, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = exec(ctx, s.q, `
		insert into settings (key, value, updated_at) values ($1, $2, now())
		on conflict (key) do update set value = excluded.value, updated_at = now()`, key, b)
	return err
}
