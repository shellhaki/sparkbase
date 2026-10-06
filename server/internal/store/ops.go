package store

import (
	"context"
	"encoding/json"
	"time"
)

// Jobs

const jobCols = `id, type, payload, status, dedupe_key, run_at, attempts, max_attempts, locked_at, last_error, finished_at, created_at`

type NewJob struct {
	Type        string
	Payload     any
	DedupeKey   string // optional: at most one queued or running job per key
	RunAt       time.Time
	MaxAttempts int
}

// Enqueue inserts a job. With a dedupe key, an existing queued or running
// job with the same key wins and Enqueue returns 0.
func (s *Store) Enqueue(ctx context.Context, j NewJob) (int64, error) {
	payload, err := json.Marshal(j.Payload)
	if err != nil {
		return 0, err
	}
	if j.Payload == nil {
		payload = []byte(`{}`)
	}
	var dedupe *string
	if j.DedupeKey != "" {
		dedupe = &j.DedupeKey
	}
	runAt := j.RunAt
	if runAt.IsZero() {
		runAt = time.Now()
	}
	max := j.MaxAttempts
	if max == 0 {
		max = 5
	}
	var id int64
	err = s.q.QueryRow(ctx, `
		insert into jobs (type, payload, dedupe_key, run_at, max_attempts)
		values ($1, $2, $3, $4, $5)
		on conflict (dedupe_key) where status in ('queued','running') do nothing
		returning id`, j.Type, payload, dedupe, runAt, max).Scan(&id)
	if err != nil {
		if notFound(err) == ErrNotFound {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// ClaimJob takes the next ready job, skipping rows other workers hold.
func (s *Store) ClaimJob(ctx context.Context, workerID string, types []string) (*Job, error) {
	return one[Job](ctx, s.q, `
		update jobs set status = 'running', locked_at = now(), locked_by = $1, attempts = attempts + 1
		where id = (
		  select id from jobs
		  where status = 'queued' and run_at <= now() and (cardinality($2::text[]) = 0 or type = any($2))
		  order by run_at, id
		  for update skip locked
		  limit 1)
		returning `+jobCols, workerID, types)
}

func (s *Store) CompleteJob(ctx context.Context, id int64) error {
	_, err := exec(ctx, s.q, `update jobs set status = 'done', finished_at = now(), locked_at = null where id = $1`, id)
	return err
}

// FailJob re-queues with backoff, or marks the job failed after max attempts.
func (s *Store) FailJob(ctx context.Context, id int64, msg string, backoff time.Duration) (final bool, err error) {
	err = s.q.QueryRow(ctx, `
		update jobs set
		  status = case when attempts >= max_attempts then 'failed' else 'queued' end,
		  finished_at = case when attempts >= max_attempts then now() end,
		  run_at = now() + make_interval(secs => $3),
		  last_error = $2, locked_at = null, locked_by = null
		where id = $1
		returning status = 'failed'`, id, msg, backoff.Seconds()).Scan(&final)
	return final, err
}

// RequeueStuck returns jobs running longer than timeout to the queue.
func (s *Store) RequeueStuck(ctx context.Context, timeout time.Duration) (int64, error) {
	return exec(ctx, s.q, `
		update jobs set status = 'queued', locked_at = null, locked_by = null,
		  last_error = coalesce(last_error, 'worker timed out')
		where status = 'running' and locked_at < now() - make_interval(secs => $1)`, timeout.Seconds())
}

func (s *Store) GetJob(ctx context.Context, id int64) (*Job, error) {
	return one[Job](ctx, s.q, `select `+jobCols+` from jobs where id = $1`, id)
}

func (s *Store) ListJobs(ctx context.Context, status string, limit int) ([]Job, error) {
	return many[Job](ctx, s.q, `
		select `+jobCols+` from jobs where ($1 = '' or status = $1) order by id desc limit $2`, status, limit)
}

type QueueStats struct {
	Queued  int `json:"queued"`
	Running int `json:"running"`
	Failed  int `json:"failed24h"`
}

func (s *Store) JobStats(ctx context.Context) (QueueStats, error) {
	var st QueueStats
	err := s.q.QueryRow(ctx, `
		select count(*) filter (where status = 'queued'),
		       count(*) filter (where status = 'running'),
		       count(*) filter (where status = 'failed' and finished_at > now() - interval '24 hours')
		from jobs`).Scan(&st.Queued, &st.Running, &st.Failed)
	return st, err
}

func (s *Store) PruneJobs(ctx context.Context, olderThan time.Duration) error {
	_, err := exec(ctx, s.q, `delete from jobs where status in ('done','failed') and finished_at < now() - make_interval(secs => $1)`, olderThan.Seconds())
	return err
}

// Audit and notifications

type AuditEvent struct {
	ActorID    string
	ProjectID  string
	ResourceID string
	Action     string
	Detail     map[string]any
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Store) Audit(ctx context.Context, e AuditEvent) error {
	var detail []byte
	if e.Detail != nil {
		detail, _ = json.Marshal(e.Detail)
	}
	_, err := exec(ctx, s.q, `
		insert into audit_log (actor_id, project_id, resource_id, action, detail) values ($1, $2, $3, $4, $5)`,
		nullable(e.ActorID), nullable(e.ProjectID), nullable(e.ResourceID), e.Action, detail)
	return err
}

func (s *Store) ListAudit(ctx context.Context, resourceID string, beforeID int64, limit int) ([]AuditEntry, error) {
	return many[AuditEntry](ctx, s.q, `
		select a.id, a.actor_id, a.project_id, a.resource_id, a.action, a.detail, a.created_at, u.email as actor_email
		from audit_log a left join users u on u.id = a.actor_id
		where ($1 = '' or a.resource_id::text = $1) and ($2 = 0 or a.id < $2)
		order by a.id desc limit $3`, resourceID, beforeID, limit)
}

func (s *Store) Notify(ctx context.Context, userID, resourceID, level, message string) error {
	_, err := exec(ctx, s.q, `
		insert into notifications (user_id, resource_id, level, message) values ($1, $2, $3, $4)`,
		userID, nullable(resourceID), level, message)
	return err
}

func (s *Store) ListNotifications(ctx context.Context, userID string, limit int) ([]Notification, error) {
	return many[Notification](ctx, s.q, `
		select id, user_id, resource_id, level, message, read_at, created_at
		from notifications where user_id = $1 order by created_at desc limit $2`, userID, limit)
}

func (s *Store) MarkNotificationsRead(ctx context.Context, userID string) error {
	_, err := exec(ctx, s.q, `update notifications set read_at = now() where user_id = $1 and read_at is null`, userID)
	return err
}

// Usage

func (s *Store) InsertUsage(ctx context.Context, resourceID string, at time.Time, metrics map[string]int64) error {
	for m, v := range metrics {
		if _, err := exec(ctx, s.q, `
			insert into usage_samples (resource_id, metric, sampled_at, value) values ($1, $2, $3, $4)
			on conflict do nothing`, resourceID, m, at, v); err != nil {
			return err
		}
	}
	return nil
}

// LatestUsage returns the most recent value of each metric.
func (s *Store) LatestUsage(ctx context.Context, resourceID string) (map[string]int64, error) {
	rows, err := s.q.Query(ctx, `
		select distinct on (metric) metric, value from usage_samples
		where resource_id = $1 order by metric, sampled_at desc`, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var m string
		var v int64
		if err := rows.Scan(&m, &v); err != nil {
			return nil, err
		}
		out[m] = v
	}
	return out, rows.Err()
}

func (s *Store) UsageSeries(ctx context.Context, resourceID, metric string, from, to time.Time) ([]UsageSample, error) {
	return many[UsageSample](ctx, s.q, `
		select metric, sampled_at, value from usage_samples
		where resource_id = $1 and ($2 = '' or metric = $2) and sampled_at between $3 and $4
		order by sampled_at`, resourceID, metric, from, to)
}

func (s *Store) PruneUsage(ctx context.Context, olderThan time.Duration) error {
	_, err := exec(ctx, s.q, `delete from usage_samples where sampled_at < now() - make_interval(secs => $1)`, olderThan.Seconds())
	return err
}

// Backups

const backupCols = `id, resource_id, status, object_key, size_bytes, checksum, expires_at, error, created_at, finished_at, destination, via_resource_id`

func (s *Store) CreateBackup(ctx context.Context, resourceID string) (*Backup, error) {
	return one[Backup](ctx, s.q, `insert into backups (resource_id) values ($1) returning `+backupCols, resourceID)
}

// CreateBackupVia records a backup taken by a backup service.
func (s *Store) CreateBackupVia(ctx context.Context, resourceID, viaID, destination string) (*Backup, error) {
	return one[Backup](ctx, s.q, `insert into backups (resource_id, via_resource_id, destination) values ($1, $2, $3) returning `+backupCols, resourceID, viaID, destination)
}

func (s *Store) ListBackupsVia(ctx context.Context, viaID string, limit int) ([]Backup, error) {
	return many[Backup](ctx, s.q, `select `+backupCols+` from backups where via_resource_id = $1 order by created_at desc limit $2`, viaID, limit)
}

func (s *Store) FinishBackup(ctx context.Context, id, key string, size int64, checksum string, expires time.Time) error {
	_, err := exec(ctx, s.q, `
		update backups set status = 'done', object_key = $2, size_bytes = $3, checksum = $4, expires_at = $5, finished_at = now()
		where id = $1`, id, key, size, checksum, expires)
	return err
}

func (s *Store) FailBackup(ctx context.Context, id, msg string) error {
	_, err := exec(ctx, s.q, `update backups set status = 'failed', error = $2, finished_at = now() where id = $1`, id, msg)
	return err
}

func (s *Store) GetBackup(ctx context.Context, id string) (*Backup, error) {
	return one[Backup](ctx, s.q, `select `+backupCols+` from backups where id = $1`, id)
}

func (s *Store) ListBackups(ctx context.Context, resourceID string) ([]Backup, error) {
	return many[Backup](ctx, s.q, `select `+backupCols+` from backups where resource_id = $1 order by created_at desc`, resourceID)
}

func (s *Store) ExpiredBackups(ctx context.Context) ([]Backup, error) {
	return many[Backup](ctx, s.q, `select `+backupCols+` from backups where status = 'done' and expires_at < now()`)
}

func (s *Store) DeleteBackup(ctx context.Context, id string) error {
	_, err := exec(ctx, s.q, `delete from backups where id = $1`, id)
	return err
}
