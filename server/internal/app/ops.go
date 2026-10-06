package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/drivers"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

// SampleUsage measures every running service and enforces its limits.
func (a *App) SampleUsage(ctx context.Context) error {
	list, err := a.Store.ListResourcesByStatus(ctx, "active", "paused")
	if err != nil {
		return err
	}
	now := a.Now().UTC().Truncate(time.Second)
	var firstErr error
	for i := range list {
		r := &list[i]
		if err := a.sampleOne(ctx, r, now); err != nil {
			a.Log.Warn("sample usage", "resource", r.ID, "err", err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (a *App) sampleOne(ctx context.Context, r *store.Resource, now time.Time) error {
	t, d, err := a.Target(ctx, r)
	if err != nil {
		return err
	}
	usage, err := d.MeasureUsage(ctx, t)
	if err != nil {
		return err
	}
	if n, err := a.Counter.Get(ctx, r.ID); err == nil {
		usage["requests_month"] = n
	}
	if err := a.Store.InsertUsage(ctx, r.ID, now, usage); err != nil {
		return err
	}
	return a.EnforceLimits(ctx, r, usage)
}

// EnforceLimits warns at 80% and 90%, locks writes at 100% and unlocks once
// usage drops back under the limit.
func (a *App) EnforceLimits(ctx context.Context, r *store.Resource, usage map[string]int64) error {
	var worst float64
	var worstLimit string
	for _, p := range LimitPairs[r.Kind] {
		lim := r.Limits.Get(p.Limit)
		if lim <= 0 {
			continue
		}
		pct := float64(usage[p.Metric]) / float64(lim*p.Factor) * 100
		if pct > worst {
			worst, worstLimit = pct, p.Limit
		}
		for _, level := range []int{80, 90} {
			key := fmt.Sprintf("%swarn:%s:%s:%d", a.Cfg.RedisPrefix, r.ID, p.Limit, level)
			if pct >= float64(level) && pct < 100 {
				if ok, _ := a.Redis.SetNX(ctx, key, 1, 7*24*time.Hour).Result(); ok {
					a.audit(ctx, "", r, "limit.warning", map[string]any{"limit": p.Limit, "percent": level})
					a.NotifyOwner(ctx, r, "warning", fmt.Sprintf("Service %s is at %d%% of its %s limit", r.Name, level, p.Limit))
				}
			} else if pct < 80 {
				a.Redis.Del(ctx, key)
			}
		}
	}

	if r.Kind == "cache" {
		if lim := r.Limits.Get("connections"); lim > 0 && usage["connections"] > lim {
			if t, _, err := a.Target(ctx, r); err == nil {
				if c, ok := a.Drivers["cache"].(*drivers.Cache); ok {
					if killed, err := c.EnforceClients(ctx, t, int(lim)); err == nil && killed > 0 {
						a.audit(ctx, "", r, "limit.clients_killed", map[string]any{"count": killed})
					}
				}
			}
		}
	}

	over := worst >= 100
	if over == r.WriteLocked {
		return nil
	}
	if err := a.SetWriteLock(ctx, r, over); err != nil {
		return err
	}
	if over {
		a.audit(ctx, "", r, "limit.breach", map[string]any{"limit": worstLimit})
		a.NotifyOwner(ctx, r, "critical", fmt.Sprintf("Service %s reached its %s limit; writes are blocked until usage drops", r.Name, worstLimit))
	} else {
		a.audit(ctx, "", r, "limit.cleared", nil)
		a.NotifyOwner(ctx, r, "info", fmt.Sprintf("Service %s is back under its limits; writes are allowed again", r.Name))
	}
	return nil
}

// SetWriteLock applies or lifts the write lock on the engine and records it.
func (a *App) SetWriteLock(ctx context.Context, r *store.Resource, locked bool) error {
	t, d, err := a.Target(ctx, r)
	if err != nil {
		return err
	}
	creds, err := a.Credentials(ctx, r.ID)
	if err != nil {
		return err
	}
	if err := d.SetWriteLock(ctx, t, creds, locked); err != nil {
		return err
	}
	r.WriteLocked = locked
	a.Tenants.Invalidate(r.ID)
	return a.Store.SetWriteLocked(ctx, r.ID, locked)
}

// PauseInactive pauses services idle for longer than the admin setting.
func (a *App) PauseInactive(ctx context.Context) error {
	s, err := a.Settings(ctx)
	if err != nil || s.AutoPauseDays <= 0 {
		return err
	}
	list, err := a.Store.ListResourcesByStatus(ctx, "active")
	if err != nil {
		return err
	}
	cutoff := a.Now().Add(-time.Duration(s.AutoPauseDays) * 24 * time.Hour)
	for i := range list {
		r := &list[i]
		last := r.CreatedAt
		if r.LastActiveAt != nil {
			last = *r.LastActiveAt
		}
		if last.After(cutoff) {
			continue
		}
		if err := a.SetPaused(ctx, "", r, true); err != nil {
			a.Log.Warn("auto pause", "resource", r.ID, "err", err.Error())
			continue
		}
		a.NotifyOwner(ctx, r, "warning", fmt.Sprintf("Service %s was paused after %d days without activity. Resume it from the dashboard.", r.Name, s.AutoPauseDays))
	}
	return nil
}

// Backups

func (a *App) backupDir() string {
	if a.Cfg.BackupDir != "" {
		return a.Cfg.BackupDir
	}
	return filepath.Join(os.TempDir(), "sparkbase-backups")
}

func pgTool(h *store.Host, name string) string {
	if h.Config.PgBinDir != "" {
		return filepath.Join(h.Config.PgBinDir, name)
	}
	for _, dir := range []string{"/opt/homebrew/opt/postgresql@18/bin", "/usr/lib/postgresql/18/bin", "/usr/local/bin"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return filepath.Join(dir, name)
		}
	}
	return name
}

// IsBackupKind reports whether a service kind has database backups.
func IsBackupKind(kind string) bool {
	return drivers.HostKindFor(kind) == "postgres"
}

// StartBackup records a backup and queues the dump.
func (a *App) StartBackup(ctx context.Context, actorID string, r *store.Resource) (*store.Backup, error) {
	b, err := a.Store.CreateBackup(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if err := a.Enqueue(ctx, "backup_resource", map[string]string{"resourceId": r.ID, "backupId": b.ID}, "backup:"+b.ID); err != nil {
		return nil, err
	}
	a.audit(ctx, actorID, r, "backup.start", map[string]any{"backup": b.ID})
	return b, nil
}

// RunBackup dumps a Postgres-family service, encrypts the dump and stores it.
func (a *App) RunBackup(ctx context.Context, resourceID, backupID string) error {
	r, err := a.Store.GetResource(ctx, resourceID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	fail := func(err error) error {
		_ = a.Store.FailBackup(ctx, backupID, err.Error())
		return nil
	}
	t, _, err := a.Target(ctx, r)
	if err != nil {
		return fail(err)
	}
	cmd := exec.CommandContext(ctx, pgTool(t.H, "pg_dump"), "--format=custom", "--no-owner", "--no-privileges",
		"--dbname", drivers.AdminURL(t.H, t.AdminSecret, r.Ext()))
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return fail(fmt.Errorf("pg_dump: %v: %s", err, lastLine(stderr.String())))
	}
	sealed := a.Box.Seal(out.Bytes())
	sum := sha256.Sum256(sealed)
	key := filepath.Join(drivers.BucketName(r.ID), backupID+".dump.enc")
	full := filepath.Join(a.backupDir(), key)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(full, sealed, 0o600); err != nil {
		return fail(err)
	}
	days := r.Limits.Get("backup_days")
	if days <= 0 {
		days = 3
	}
	if err := a.Store.FinishBackup(ctx, backupID, key, int64(len(sealed)), hex.EncodeToString(sum[:]),
		a.Now().Add(time.Duration(days)*24*time.Hour)); err != nil {
		return err
	}
	a.audit(ctx, "", r, "backup.done", map[string]any{"backup": backupID, "bytes": len(sealed)})
	return nil
}

func lastLine(s string) string {
	s = string(bytes.TrimSpace([]byte(s)))
	if i := bytes.LastIndexByte([]byte(s), '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// RestoreBackup replaces a service's data with a backup.
func (a *App) RestoreBackup(ctx context.Context, actorID string, r *store.Resource, b *store.Backup) error {
	if b.Status != "done" || b.ObjectKey == nil {
		return fmt.Errorf("backup is not complete")
	}
	sealed, err := os.ReadFile(filepath.Join(a.backupDir(), *b.ObjectKey))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(sealed)
	if b.Checksum != nil && hex.EncodeToString(sum[:]) != *b.Checksum {
		return fmt.Errorf("backup checksum does not match")
	}
	dump, err := a.Box.Open(sealed)
	if err != nil {
		return fmt.Errorf("decrypt backup: %w", err)
	}
	t, _, err := a.Target(ctx, r)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, pgTool(t.H, "pg_restore"), "--clean", "--if-exists", "--no-owner",
		"--no-privileges", "--single-transaction", "--role", drivers.OwnerRole(r),
		"--dbname", drivers.AdminURL(t.H, t.AdminSecret, r.Ext()))
	cmd.Stdin = bytes.NewReader(dump)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore: %v: %s", err, lastLine(stderr.String()))
	}
	a.Tenants.Invalidate(r.ID)
	a.audit(ctx, actorID, r, "backup.restore", map[string]any{"backup": b.ID})
	return nil
}

// ScheduleBackups queues a nightly backup for every running Postgres-family service.
func (a *App) ScheduleBackups(ctx context.Context) error {
	list, err := a.Store.ListResourcesByStatus(ctx, "active", "paused")
	if err != nil {
		return err
	}
	for i := range list {
		if IsBackupKind(list[i].Kind) {
			if _, err := a.StartBackup(ctx, "", &list[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// Housekeeping prunes old rows and expired backups.
func (a *App) Housekeeping(ctx context.Context) error {
	if err := a.Store.PruneJobs(ctx, 7*24*time.Hour); err != nil {
		return err
	}
	if err := a.Store.PruneUsage(ctx, 31*24*time.Hour); err != nil {
		return err
	}
	if err := a.Store.DeleteExpiredSessions(ctx); err != nil {
		return err
	}
	expired, err := a.Store.ExpiredBackups(ctx)
	if err != nil {
		return err
	}
	for _, b := range expired {
		if b.ObjectKey != nil {
			_ = os.Remove(filepath.Join(a.backupDir(), *b.ObjectKey))
		}
		if err := a.Store.DeleteBackup(ctx, b.ID); err != nil {
			return err
		}
	}
	return nil
}
