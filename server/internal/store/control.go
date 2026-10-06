package store

import (
	"context"
	"encoding/json"
	"time"
)

// Cron

type CronJob struct {
	ID         string          `db:"id" json:"id"`
	ResourceID string          `db:"resource_id" json:"-"`
	Name       string          `db:"name" json:"name"`
	Schedule   string          `db:"schedule" json:"schedule"`
	Action     json.RawMessage `db:"action" json:"action"`
	Enabled    bool            `db:"enabled" json:"enabled"`
	NextRunAt  time.Time       `db:"next_run_at" json:"nextRunAt"`
	LastRunAt  *time.Time      `db:"last_run_at" json:"lastRunAt"`
	LastStatus *string         `db:"last_status" json:"lastStatus"`
	CreatedAt  time.Time       `db:"created_at" json:"createdAt"`
}

type CronRun struct {
	ID         int64      `db:"id" json:"id"`
	JobID      string     `db:"job_id" json:"jobId"`
	StartedAt  time.Time  `db:"started_at" json:"startedAt"`
	FinishedAt *time.Time `db:"finished_at" json:"finishedAt"`
	Status     string     `db:"status" json:"status"`
	Output     *string    `db:"output" json:"output"`
}

const cronCols = `id, resource_id, name, schedule, action, enabled, next_run_at, last_run_at, last_status, created_at`

func (s *Store) ListCronJobs(ctx context.Context, resourceID string) ([]CronJob, error) {
	return many[CronJob](ctx, s.q, `select `+cronCols+` from cron_jobs where resource_id = $1 order by name`, resourceID)
}

func (s *Store) GetCronJob(ctx context.Context, resourceID, id string) (*CronJob, error) {
	return one[CronJob](ctx, s.q, `select `+cronCols+` from cron_jobs where resource_id = $1 and id::text = $2`, resourceID, id)
}

func (s *Store) CreateCronJob(ctx context.Context, resourceID, name, schedule string, action json.RawMessage, next time.Time) (*CronJob, error) {
	j, err := one[CronJob](ctx, s.q, `
		insert into cron_jobs (resource_id, name, schedule, action, next_run_at) values ($1, $2, $3, $4, $5)
		returning `+cronCols, resourceID, name, schedule, action, next)
	if IsUniqueViolation(err) {
		return nil, ErrConflict
	}
	return j, err
}

func (s *Store) UpdateCronJob(ctx context.Context, id string, schedule *string, action json.RawMessage, enabled *bool, next *time.Time) error {
	_, err := exec(ctx, s.q, `
		update cron_jobs set schedule = coalesce($2, schedule), action = coalesce($3, action),
		  enabled = coalesce($4, enabled), next_run_at = coalesce($5, next_run_at)
		where id = $1`, id, schedule, nullJSON(action), enabled, next)
	return err
}

func nullJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func (s *Store) DeleteCronJob(ctx context.Context, resourceID, id string) (bool, error) {
	n, err := exec(ctx, s.q, `delete from cron_jobs where resource_id = $1 and id::text = $2`, resourceID, id)
	return n > 0, err
}

func (s *Store) CountCronJobs(ctx context.Context, resourceID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `select count(*) from cron_jobs where resource_id = $1`, resourceID).Scan(&n)
	return n, err
}

// ClaimDueCronJobs takes due jobs of active cron services and moves their
// next run forward with next(job), so other workers skip them.
func (s *Store) ClaimDueCronJobs(ctx context.Context, next func(CronJob) time.Time) ([]CronJob, error) {
	var claimed []CronJob
	err := s.Tx(ctx, func(tx *Store) error {
		jobs, err := many[CronJob](ctx, tx.q, `
			select j.id, j.resource_id, j.name, j.schedule, j.action, j.enabled, j.next_run_at, j.last_run_at, j.last_status, j.created_at
			from cron_jobs j join resources r on r.id = j.resource_id
			where j.enabled and j.next_run_at <= now() and r.status = 'active'
			order by j.next_run_at limit 50
			for update of j skip locked`)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			if _, err := exec(ctx, tx.q, `update cron_jobs set next_run_at = $2, last_run_at = now() where id = $1`, j.ID, next(j)); err != nil {
				return err
			}
		}
		claimed = jobs
		return nil
	})
	return claimed, err
}

func (s *Store) StartCronRun(ctx context.Context, jobID string) (int64, error) {
	var id int64
	err := s.q.QueryRow(ctx, `insert into cron_runs (job_id) values ($1) returning id`, jobID).Scan(&id)
	return id, err
}

func (s *Store) FinishCronRun(ctx context.Context, runID int64, jobID, status, output string) error {
	if len(output) > 4000 {
		output = output[:4000]
	}
	if _, err := exec(ctx, s.q, `update cron_runs set status = $2, output = $3, finished_at = now() where id = $1`, runID, status, output); err != nil {
		return err
	}
	_, err := exec(ctx, s.q, `update cron_jobs set last_status = $2 where id = $1`, jobID, status)
	return err
}

func (s *Store) ListCronRuns(ctx context.Context, jobID string, limit int) ([]CronRun, error) {
	return many[CronRun](ctx, s.q, `
		select id, job_id, started_at, finished_at, status, output from cron_runs
		where job_id = $1 order by started_at desc limit $2`, jobID, limit)
}

// Monitors

type Monitor struct {
	ID              string    `db:"id" json:"id"`
	ResourceID      string    `db:"resource_id" json:"-"`
	Name            string    `db:"name" json:"name"`
	URL             string    `db:"url" json:"url"`
	Method          string    `db:"method" json:"method"`
	IntervalSeconds int       `db:"interval_seconds" json:"intervalSeconds"`
	TimeoutSeconds  int       `db:"timeout_seconds" json:"timeoutSeconds"`
	ExpectStatus    int       `db:"expect_status" json:"expectStatus"`
	Enabled         bool      `db:"enabled" json:"enabled"`
	State           string    `db:"state" json:"state"`
	NextCheckAt     time.Time `db:"next_check_at" json:"nextCheckAt"`
	CreatedAt       time.Time `db:"created_at" json:"createdAt"`
}

type MonitorResult struct {
	MonitorID string    `db:"monitor_id" json:"-"`
	CheckedAt time.Time `db:"checked_at" json:"checkedAt"`
	OK        bool      `db:"ok" json:"ok"`
	Status    *int      `db:"status" json:"status"`
	LatencyMs *int      `db:"latency_ms" json:"latencyMs"`
	Error     *string   `db:"error" json:"error"`
}

const monitorCols = `id, resource_id, name, url, method, interval_seconds, timeout_seconds, expect_status, enabled, state, next_check_at, created_at`

func (s *Store) ListMonitors(ctx context.Context, resourceID string) ([]Monitor, error) {
	return many[Monitor](ctx, s.q, `select `+monitorCols+` from monitors where resource_id = $1 order by name`, resourceID)
}

func (s *Store) GetMonitor(ctx context.Context, resourceID, id string) (*Monitor, error) {
	return one[Monitor](ctx, s.q, `select `+monitorCols+` from monitors where resource_id = $1 and id::text = $2`, resourceID, id)
}

func (s *Store) CreateMonitor(ctx context.Context, m Monitor) (*Monitor, error) {
	out, err := one[Monitor](ctx, s.q, `
		insert into monitors (resource_id, name, url, method, interval_seconds, timeout_seconds, expect_status)
		values ($1, $2, $3, $4, $5, $6, $7) returning `+monitorCols,
		m.ResourceID, m.Name, m.URL, m.Method, m.IntervalSeconds, m.TimeoutSeconds, m.ExpectStatus)
	if IsUniqueViolation(err) {
		return nil, ErrConflict
	}
	return out, err
}

func (s *Store) UpdateMonitor(ctx context.Context, id string, enabled *bool, interval *int, url *string) error {
	_, err := exec(ctx, s.q, `
		update monitors set enabled = coalesce($2, enabled), interval_seconds = coalesce($3, interval_seconds), url = coalesce($4, url),
		  next_check_at = now()
		where id = $1`, id, enabled, interval, url)
	return err
}

func (s *Store) DeleteMonitor(ctx context.Context, resourceID, id string) (bool, error) {
	n, err := exec(ctx, s.q, `delete from monitors where resource_id = $1 and id::text = $2`, resourceID, id)
	return n > 0, err
}

func (s *Store) CountMonitors(ctx context.Context, resourceID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `select count(*) from monitors where resource_id = $1`, resourceID).Scan(&n)
	return n, err
}

// ClaimDueMonitors takes due checks of active monitor services.
func (s *Store) ClaimDueMonitors(ctx context.Context) ([]Monitor, error) {
	var claimed []Monitor
	err := s.Tx(ctx, func(tx *Store) error {
		list, err := many[Monitor](ctx, tx.q, `
			select m.id, m.resource_id, m.name, m.url, m.method, m.interval_seconds, m.timeout_seconds, m.expect_status, m.enabled, m.state, m.next_check_at, m.created_at
			from monitors m join resources r on r.id = m.resource_id
			where m.enabled and m.next_check_at <= now() and r.status = 'active'
			order by m.next_check_at limit 100
			for update of m skip locked`)
		if err != nil {
			return err
		}
		for _, m := range list {
			if _, err := exec(ctx, tx.q, `update monitors set next_check_at = now() + make_interval(secs => interval_seconds) where id = $1`, m.ID); err != nil {
				return err
			}
		}
		claimed = list
		return nil
	})
	return claimed, err
}

// RecordCheck stores a result and returns the previous state.
func (s *Store) RecordCheck(ctx context.Context, r MonitorResult) (prev string, err error) {
	if _, err = exec(ctx, s.q, `
		insert into monitor_results (monitor_id, checked_at, ok, status, latency_ms, error) values ($1, $2, $3, $4, $5, $6)
		on conflict do nothing`, r.MonitorID, r.CheckedAt, r.OK, r.Status, r.LatencyMs, r.Error); err != nil {
		return "", err
	}
	state := "down"
	if r.OK {
		state = "up"
	}
	err = s.q.QueryRow(ctx, `
		update monitors m set state = $2 from (select state from monitors where id = $1) old
		where m.id = $1 returning old.state`, r.MonitorID, state).Scan(&prev)
	return prev, err
}

func (s *Store) MonitorResults(ctx context.Context, id string, since time.Time, limit int) ([]MonitorResult, error) {
	return many[MonitorResult](ctx, s.q, `
		select monitor_id, checked_at, ok, status, latency_ms, error from monitor_results
		where monitor_id = $1 and checked_at >= $2 order by checked_at desc limit $3`, id, since, limit)
}

// Uptime is the share of good checks since a time, from 0 to 1, or -1 with no data.
func (s *Store) Uptime(ctx context.Context, id string, since time.Time) (float64, error) {
	var total, good int
	err := s.q.QueryRow(ctx, `select count(*), count(*) filter (where ok) from monitor_results where monitor_id = $1 and checked_at >= $2`, id, since).Scan(&total, &good)
	if err != nil || total == 0 {
		return -1, err
	}
	return float64(good) / float64(total), nil
}

func (s *Store) PruneMonitorResults(ctx context.Context, olderThan time.Duration) error {
	_, err := exec(ctx, s.q, `delete from monitor_results where checked_at < now() - make_interval(secs => $1)`, olderThan.Seconds())
	return err
}

// ResourcesByKind returns services of a kind in a status.
func (s *Store) ResourcesByKind(ctx context.Context, kind, status string) ([]Resource, error) {
	return many[Resource](ctx, s.q, `select `+resourceCols+resourceFrom+`where r.kind = $1 and r.status = $2`, kind, status)
}
