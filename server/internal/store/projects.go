package store

import (
	"context"
	"time"
)

const projectCols = `id, owner_id, slug, name, status, created_at`

func (s *Store) CreateProject(ctx context.Context, ownerID, slug, name string) (*Project, error) {
	p, err := one[Project](ctx, s.q, `
		insert into projects (owner_id, slug, name) values ($1, $2, $3) returning `+projectCols,
		ownerID, slug, name)
	if IsUniqueViolation(err) {
		return nil, ErrConflict
	}
	return p, err
}

func (s *Store) GetProject(ctx context.Context, id string) (*Project, error) {
	return one[Project](ctx, s.q, `select `+projectCols+` from projects where id = $1`, id)
}

func (s *Store) GetProjectBySlug(ctx context.Context, slug string) (*Project, error) {
	return one[Project](ctx, s.q, `select `+projectCols+` from projects where slug = $1`, slug)
}

// ListProjects returns the owner's projects, or every project when ownerID is empty.
func (s *Store) ListProjects(ctx context.Context, ownerID string) ([]Project, error) {
	if ownerID == "" {
		return many[Project](ctx, s.q, `select `+projectCols+` from projects order by created_at desc`)
	}
	return many[Project](ctx, s.q, `select `+projectCols+` from projects where owner_id = $1 order by created_at desc`, ownerID)
}

func (s *Store) RenameProject(ctx context.Context, id, name string) error {
	_, err := exec(ctx, s.q, `update projects set name = $2 where id = $1`, id, name)
	return err
}

func (s *Store) SetProjectStatus(ctx context.Context, id, status string) error {
	_, err := exec(ctx, s.q, `update projects set status = $2 where id = $1`, id, status)
	return err
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	_, err := exec(ctx, s.q, `delete from projects where id = $1`, id)
	return err
}

// Project keys

func (s *Store) CreateProjectKey(ctx context.Context, projectID, kind, prefix, hash string, enc []byte) error {
	_, err := exec(ctx, s.q, `
		insert into project_keys (project_id, kind, prefix, key_hash, key_enc) values ($1, $2, $3, $4, $5)`,
		projectID, kind, prefix, hash, enc)
	return err
}

// RevokeProjectKeys revokes every active key of one kind for a project.
func (s *Store) RevokeProjectKeys(ctx context.Context, projectID, kind string) error {
	_, err := exec(ctx, s.q, `update project_keys set revoked_at = now() where project_id = $1 and kind = $2 and revoked_at is null`, projectID, kind)
	return err
}

func (s *Store) ActiveProjectKeys(ctx context.Context, projectID string) ([]ProjectKey, error) {
	return many[ProjectKey](ctx, s.q, `
		select id, project_id, kind, prefix, key_enc, created_at, revoked_at
		from project_keys where project_id = $1 and revoked_at is null order by kind`, projectID)
}

// ProjectKeyByHash returns the active key and its project.
func (s *Store) ProjectKeyByHash(ctx context.Context, hash string) (*ProjectKey, error) {
	return one[ProjectKey](ctx, s.q, `
		select id, project_id, kind, prefix, key_enc, created_at, revoked_at
		from project_keys where key_hash = $1 and revoked_at is null`, hash)
}

// Account API keys

const apiKeyCols = `id, user_id, project_id, name, prefix, scopes, last_used_at, expires_at, revoked_at, created_at`

func (s *Store) CreateAPIKey(ctx context.Context, userID string, projectID *string, name, prefix, hash string, expires *time.Time) (*APIKey, error) {
	return one[APIKey](ctx, s.q, `
		insert into api_keys (user_id, project_id, name, prefix, key_hash, expires_at)
		values ($1, $2, $3, $4, $5, $6) returning `+apiKeyCols, userID, projectID, name, prefix, hash, expires)
}

func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	return many[APIKey](ctx, s.q, `select `+apiKeyCols+` from api_keys where user_id = $1 and revoked_at is null order by created_at desc`, userID)
}

func (s *Store) RevokeAPIKey(ctx context.Context, userID, id string) (bool, error) {
	n, err := exec(ctx, s.q, `update api_keys set revoked_at = now() where id = $1 and user_id = $2 and revoked_at is null`, id, userID)
	return n > 0, err
}

// APIKeyByHash returns a key that is not revoked and not expired.
func (s *Store) APIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	k, err := one[APIKey](ctx, s.q, `
		update api_keys set last_used_at = now()
		where key_hash = $1 and revoked_at is null and (expires_at is null or expires_at > now())
		returning `+apiKeyCols, hash)
	return k, err
}
