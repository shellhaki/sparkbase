// Package worker runs background jobs from the jobs table and enqueues the
// periodic ones.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/app"
	"github.com/shellhaki/sparkbase/server/internal/mail"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

// Handler runs one job. final is true on the last allowed attempt.
type Handler func(ctx context.Context, payload json.RawMessage) error

// FailHook runs once a job has failed for good.
type FailHook func(ctx context.Context, payload json.RawMessage, err error)

type Periodic struct {
	Type  string
	Every time.Duration
}

type Worker struct {
	App          *app.App
	ID           string
	Concurrency  int
	PollInterval time.Duration
	JobTimeout   time.Duration
	handlers     map[string]Handler
	failHooks    map[string]FailHook
	periodic     []Periodic
}

func New(a *app.App) *Worker {
	host, _ := os.Hostname()
	w := &Worker{
		App: a, ID: host + "-" + sbcrypto.RandomLower(6), Concurrency: 4,
		PollInterval: time.Second, JobTimeout: 10 * time.Minute,
		handlers: map[string]Handler{}, failHooks: map[string]FailHook{},
	}
	w.registerDefaults()
	return w
}

func (w *Worker) Handle(typ string, h Handler)     { w.handlers[typ] = h }
func (w *Worker) OnFail(typ string, h FailHook)    { w.failHooks[typ] = h }
func (w *Worker) Every(typ string, d time.Duration) { w.periodic = append(w.periodic, Periodic{typ, d}) }

func (w *Worker) types() []string {
	out := make([]string, 0, len(w.handlers))
	for t := range w.handlers {
		out = append(out, t)
	}
	return out
}

// Backoff grows exponentially from 2 seconds, capped at 10 minutes.
func Backoff(attempt int) time.Duration {
	d := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}

// RunOnce claims and runs one job. It reports whether a job was found.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	job, err := w.App.Store.ClaimJob(ctx, w.ID, w.types())
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	h := w.handlers[job.Type]
	jctx, cancel := context.WithTimeout(ctx, w.JobTimeout)
	runErr := func() (err error) {
		defer func() {
			if v := recover(); v != nil {
				err = fmt.Errorf("panic: %v", v)
			}
		}()
		return h(jctx, job.Payload)
	}()
	cancel()
	// Use a fresh context so a shutdown still records the result.
	rctx, rcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer rcancel()
	if runErr == nil {
		return true, w.App.Store.CompleteJob(rctx, job.ID)
	}
	msg := runErr.Error()
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	final, err := w.App.Store.FailJob(rctx, job.ID, msg, Backoff(job.Attempts))
	if err != nil {
		return true, err
	}
	w.App.Log.Warn("job failed", "type", job.Type, "id", job.ID, "attempt", job.Attempts, "final", final, "err", msg)
	if final {
		if hook := w.failHooks[job.Type]; hook != nil {
			hook(rctx, job.Payload, runErr)
		}
	}
	return true, nil
}

// Drain runs jobs until none are ready. Used by tests and the CLI.
func (w *Worker) Drain(ctx context.Context) error {
	for {
		found, err := w.RunOnce(ctx)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
	}
}

// Run polls for jobs with Concurrency loops and enqueues periodic jobs,
// until ctx is cancelled. Running jobs finish before Run returns.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < w.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				found, err := w.RunOnce(context.WithoutCancel(ctx))
				if err != nil {
					w.App.Log.Error("worker", "err", err.Error())
				}
				if !found || err != nil {
					select {
					case <-ctx.Done():
					case <-time.After(w.PollInterval):
					}
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.schedule(ctx)
	}()
	wg.Wait()
}

// schedule enqueues periodic jobs. The dedupe key holds one slot per period,
// so several workers never double-schedule.
func (w *Worker) schedule(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		now := time.Now()
		for _, p := range w.periodic {
			slot := now.Truncate(p.Every).Unix()
			key := fmt.Sprintf("periodic:%s:%d", p.Type, slot)
			if _, err := w.App.Store.Enqueue(ctx, store.NewJob{Type: p.Type, DedupeKey: key, MaxAttempts: 1}); err != nil && ctx.Err() == nil {
				w.App.Log.Error("schedule", "type", p.Type, "err", err.Error())
			}
		}
		if n, err := w.App.Store.RequeueStuck(ctx, w.JobTimeout+time.Minute); err == nil && n > 0 {
			w.App.Log.Warn("requeued stuck jobs", "count", n)
		}
		w.App.Tenants.CloseIdle(5 * time.Minute)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

type resourcePayload struct {
	ResourceID string `json:"resourceId"`
	ProjectID  string `json:"projectId"`
}

func decode(p json.RawMessage) resourcePayload {
	var v resourcePayload
	_ = json.Unmarshal(p, &v)
	return v
}

func (w *Worker) registerDefaults() {
	a := w.App
	w.Handle("provision_resource", func(ctx context.Context, p json.RawMessage) error {
		return a.ProvisionResource(ctx, decode(p).ResourceID)
	})
	w.OnFail("provision_resource", func(ctx context.Context, p json.RawMessage, err error) {
		a.ProvisionFailed(ctx, decode(p).ResourceID, err.Error())
	})
	w.Handle("deprovision_resource", func(ctx context.Context, p json.RawMessage) error {
		return a.DeprovisionResource(ctx, decode(p).ResourceID)
	})
	w.Handle("delete_project", func(ctx context.Context, p json.RawMessage) error {
		return a.DeleteProject(ctx, decode(p).ProjectID)
	})
	w.Handle("revoke_expired_credentials", func(ctx context.Context, _ json.RawMessage) error {
		return a.RevokeDue(ctx)
	})
	w.Handle("sample_usage", func(ctx context.Context, _ json.RawMessage) error {
		return a.SampleUsage(ctx)
	})
	w.Handle("pause_inactive", func(ctx context.Context, _ json.RawMessage) error {
		return a.PauseInactive(ctx)
	})
	w.Handle("backup_resource", func(ctx context.Context, p json.RawMessage) error {
		var v struct {
			ResourceID string `json:"resourceId"`
			BackupID   string `json:"backupId"`
		}
		_ = json.Unmarshal(p, &v)
		return a.RunBackup(ctx, v.ResourceID, v.BackupID)
	})
	w.Handle("nightly_backups", func(ctx context.Context, _ json.RawMessage) error {
		return a.ScheduleBackups(ctx)
	})
	w.Handle("housekeeping", func(ctx context.Context, _ json.RawMessage) error {
		return a.Housekeeping(ctx)
	})
	w.Handle("send_email", func(ctx context.Context, p json.RawMessage) error {
		var m mail.Message
		var v struct{ To, Subject, Text, Link string }
		if err := json.Unmarshal(p, &v); err != nil {
			return err
		}
		m = mail.Message{To: v.To, Subject: v.Subject, Text: v.Text, Link: v.Link}
		return a.Mail.Send(ctx, m)
	})

	w.Every("revoke_expired_credentials", time.Minute)
	w.Every("sample_usage", time.Minute)
	w.Every("pause_inactive", time.Hour)
	w.Every("nightly_backups", 24*time.Hour)
	w.Every("housekeeping", time.Hour)
}
