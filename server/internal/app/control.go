package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	b64 "encoding/base64"
	"github.com/shellhaki/sparkbase/server/internal/cronexpr"
	"github.com/shellhaki/sparkbase/server/internal/logs"
	"github.com/shellhaki/sparkbase/server/internal/netguard"
	"github.com/shellhaki/sparkbase/server/internal/s3"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

func (a *App) svcLog(ctx context.Context, r *store.Resource, source, level, msg string, fields map[string]any) {
	a.Logs.Push(ctx, logs.ServiceStream(r.ID), logs.Entry{Level: level, Source: source, Message: msg, Fields: fields})
}

// Cron

// CronAction is what a cron job does.
type CronAction struct {
	Type      string            `json:"type"` // http, sql, queue
	URL       string            `json:"url,omitempty"`
	Method    string            `json:"method,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      json.RawMessage   `json:"body,omitempty"`
	Postgres  string            `json:"postgres,omitempty"`
	SQL       string            `json:"sql,omitempty"`
	Queue     string            `json:"queue,omitempty"`     // queue service name
	QueueName string            `json:"queueName,omitempty"` // queue inside the service
}

// ValidateCronAction checks an action before it is saved.
func ValidateCronAction(act CronAction) error {
	switch act.Type {
	case "http":
		if err := netguard.CheckURL(act.URL); err != nil {
			return err
		}
		switch strings.ToUpper(act.Method) {
		case "", "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return errors.New("method must be GET, POST, PUT, PATCH or DELETE")
		}
	case "sql":
		if act.Postgres == "" || strings.TrimSpace(act.SQL) == "" {
			return errors.New("sql actions need postgres (service name) and sql")
		}
	case "queue":
		if act.Queue == "" || act.QueueName == "" {
			return errors.New("queue actions need queue (service name) and queueName")
		}
	default:
		return errors.New("action type must be http, sql or queue")
	}
	return nil
}

// CronTick runs every due job once.
func (a *App) CronTick(ctx context.Context) error {
	jobs, err := a.Store.ClaimDueCronJobs(ctx, func(j store.CronJob) time.Time {
		s, err := cronexpr.Parse(j.Schedule)
		if err != nil {
			return a.Now().Add(24 * time.Hour)
		}
		return s.Next(a.Now())
	})
	if err != nil {
		return err
	}
	for _, j := range jobs {
		a.RunCronJob(ctx, j)
	}
	return nil
}

// RunCronJob executes one job and records the run.
func (a *App) RunCronJob(ctx context.Context, j store.CronJob) (status, output string) {
	r, err := a.Store.GetResource(ctx, j.ResourceID)
	if err != nil {
		return "failed", err.Error()
	}
	runID, err := a.Store.StartCronRun(ctx, j.ID)
	if err != nil {
		return "failed", err.Error()
	}
	var act CronAction
	_ = json.Unmarshal(j.Action, &act)
	start := time.Now()
	jctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	output, err = a.runCronAction(jctx, r, act)
	cancel()
	status = "ok"
	level := "info"
	if err != nil {
		status, level = "failed", "error"
		output = strings.TrimSpace(output + "\n" + err.Error())
	}
	_ = a.Store.FinishCronRun(ctx, runID, j.ID, status, output)
	a.svcLog(ctx, r, "cron", level, fmt.Sprintf("%s %s", j.Name, status), map[string]any{"ms": time.Since(start).Milliseconds(), "type": act.Type, "output": truncate(output, 300)})
	return status, output
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (a *App) runCronAction(ctx context.Context, r *store.Resource, act CronAction) (string, error) {
	switch act.Type {
	case "http":
		st, _ := a.Settings(ctx)
		method := strings.ToUpper(act.Method)
		if method == "" {
			method = "POST"
		}
		var body io.Reader
		if len(act.Body) > 0 && string(act.Body) != "null" {
			body = bytes.NewReader(act.Body)
		}
		req, err := http.NewRequestWithContext(ctx, method, act.URL, body)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "Sparkbase-Cron/1")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range act.Headers {
			req.Header.Set(k, v)
		}
		res, err := netguard.Client(30*time.Second, st.AllowPrivateTargets).Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		out := fmt.Sprintf("HTTP %d\n%s", res.StatusCode, b)
		if res.StatusCode >= 400 {
			return out, fmt.Errorf("the endpoint answered %d", res.StatusCode)
		}
		return out, nil
	case "sql":
		pg, err := a.Store.GetResourceByName(ctx, r.ProjectID, "postgres", act.Postgres)
		if err != nil {
			return "", fmt.Errorf("no postgres service named %s in this project", act.Postgres)
		}
		if pg.Status != "active" {
			return "", fmt.Errorf("postgres service %s is %s", pg.Name, pg.Status)
		}
		pool, err := a.Tenants.PG(ctx, pg)
		if err != nil {
			return "", err
		}
		tag, err := pool.Exec(ctx, act.SQL)
		if err != nil {
			return "", err
		}
		return tag.String(), nil
	case "queue":
		q, err := a.Store.GetResourceByName(ctx, r.ProjectID, "queue", act.Queue)
		if err != nil {
			return "", fmt.Errorf("no queue service named %s in this project", act.Queue)
		}
		pool, err := a.Tenants.PG(ctx, q)
		if err != nil {
			return "", err
		}
		body := act.Body
		if len(body) == 0 {
			body = json.RawMessage(`{}`)
		}
		var id int64
		err = pool.QueryRow(ctx, `insert into sb_messages (queue, body) select name, $2 from sb_queues where name = $1 returning id`, act.QueueName, string(body)).Scan(&id)
		if err != nil {
			return "", fmt.Errorf("queue %s: %w", act.QueueName, err)
		}
		return fmt.Sprintf("sent message %d", id), nil
	}
	return "", errors.New("unknown action")
}

// Monitors

// MonitorTick runs every due uptime check.
func (a *App) MonitorTick(ctx context.Context) error {
	list, err := a.Store.ClaimDueMonitors(ctx)
	if err != nil {
		return err
	}
	st, _ := a.Settings(ctx)
	for _, m := range list {
		a.RunCheck(ctx, m, st.AllowPrivateTargets)
	}
	return nil
}

// RunCheck performs one check, stores it and alerts on state changes.
func (a *App) RunCheck(ctx context.Context, m store.Monitor, allowPrivate bool) store.MonitorResult {
	res := store.MonitorResult{MonitorID: m.ID, CheckedAt: a.Now().UTC().Truncate(time.Millisecond)}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, m.Method, m.URL, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Sparkbase-Monitor/1")
		var resp *http.Response
		resp, err = netguard.Client(time.Duration(m.TimeoutSeconds)*time.Second, allowPrivate).Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			code := resp.StatusCode
			res.Status = &code
			res.OK = code == m.ExpectStatus
			if !res.OK {
				msg := fmt.Sprintf("expected %d, got %d", m.ExpectStatus, code)
				res.Error = &msg
			}
		}
	}
	ms := int(time.Since(start).Milliseconds())
	res.LatencyMs = &ms
	if err != nil {
		msg := err.Error()
		res.Error = &msg
	}
	prev, err := a.Store.RecordCheck(ctx, res)
	if err != nil {
		return res
	}
	r, err := a.Store.GetResource(ctx, m.ResourceID)
	if err != nil {
		return res
	}
	now := "down"
	if res.OK {
		now = "up"
	}
	level := "info"
	if !res.OK {
		level = "warn"
	}
	a.svcLog(ctx, r, "monitor", level, fmt.Sprintf("%s %s", m.Name, now), map[string]any{"ms": ms, "status": res.Status, "error": res.Error})
	if prev != now && prev != "" && (prev != "unknown" || now == "down") {
		msg := fmt.Sprintf("%s is %s (%s)", m.Name, strings.ToUpper(now), m.URL)
		lvl := "warning"
		if now == "up" {
			lvl = "info"
		}
		a.NotifyOwner(ctx, r, lvl, msg)
		a.alertWebhook(ctx, r, map[string]any{"check": m.Name, "url": m.URL, "state": now, "at": res.CheckedAt, "error": res.Error})
	}
	return res
}

// MonitorSettings are a monitor service's settings.
type MonitorSettings struct {
	AlertWebhook string `json:"alertWebhook,omitempty"`
}

func (a *App) alertWebhook(ctx context.Context, r *store.Resource, payload map[string]any) {
	var ms MonitorSettings
	_ = json.Unmarshal(r.Settings, &ms)
	if ms.AlertWebhook == "" {
		return
	}
	st, _ := a.Settings(ctx)
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", ms.AlertWebhook, bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if res, err := netguard.Client(10*time.Second, st.AllowPrivateTargets).Do(req); err == nil {
		res.Body.Close()
	}
}

// Backup service

// BackupDestination is where a backup service stores dumps.
type BackupDestination struct {
	Type string `json:"type"` // local or s3
	// Provider is "r2" for Cloudflare R2; the endpoint is then derived from the account.
	Provider     string `json:"provider,omitempty"`
	AccountID    string `json:"accountId,omitempty"`
	Jurisdiction string `json:"jurisdiction,omitempty"` // R2 only: "" or "eu"
	Endpoint     string `json:"endpoint,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	Region       string `json:"region,omitempty"`
	Prefix       string `json:"prefix,omitempty"`
	AccessKey    string `json:"accessKey,omitempty"`
	SecretEnc    string `json:"secretEnc,omitempty"` // encrypted, never returned
}

// BackupSettings are a backup service's settings.
type BackupSettings struct {
	Schedule      string            `json:"schedule"`
	RetentionDays int               `json:"retentionDays"`
	Targets       []string          `json:"targets"`
	Destination   BackupDestination `json:"destination"`
	NextRunAt     *time.Time        `json:"nextRunAt,omitempty"`
}

func (a *App) BackupSettingsOf(r *store.Resource) BackupSettings {
	bs := BackupSettings{Schedule: "0 3 * * *", RetentionDays: 7, Destination: BackupDestination{Type: "local"}}
	_ = json.Unmarshal(r.Settings, &bs)
	if bs.Destination.Type == "" {
		bs.Destination.Type = "local"
	}
	return bs
}

func (a *App) backupS3(bs BackupSettings) (*s3.Client, string, string, error) {
	d := bs.Destination
	if d.Type != "s3" {
		return nil, "", "", errors.New("the destination is not S3")
	}
	secret := ""
	if d.SecretEnc != "" {
		raw, err := decodeB64(d.SecretEnc)
		if err != nil {
			return nil, "", "", err
		}
		if secret, err = a.Box.OpenString(raw); err != nil {
			return nil, "", "", err
		}
	}
	region := d.Region
	if region == "" {
		region = "us-east-1"
	}
	prefix := strings.Trim(d.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return &s3.Client{Endpoint: d.Endpoint, Region: region, AccessKey: d.AccessKey, SecretKey: secret}, d.Bucket, prefix, nil
}

var r2Account = regexp.MustCompile(`^[0-9a-f]{32}$`)

// R2Endpoint is the S3 endpoint of a Cloudflare account's R2 buckets.
func R2Endpoint(accountID, jurisdiction string) (string, error) {
	if !r2Account.MatchString(accountID) {
		return "", errors.New("the R2 account ID is 32 hex characters, shown on the R2 overview page")
	}
	switch jurisdiction {
	case "":
		return "https://" + accountID + ".r2.cloudflarestorage.com", nil
	case "eu":
		return "https://" + accountID + ".eu.r2.cloudflarestorage.com", nil
	}
	return "", errors.New(`jurisdiction must be empty or "eu"`)
}

// CheckBackupDestination writes and deletes a small object, so wrong keys or
// a missing bucket show up when the settings are saved, not at 3 a.m.
func (a *App) CheckBackupDestination(ctx context.Context, bs BackupSettings) error {
	c, bucket, prefix, err := a.backupS3(bs)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	key := prefix + ".sparkbase-check"
	if err := c.Put(ctx, bucket, key, strings.NewReader("ok"), 2, "text/plain"); err != nil {
		return fmt.Errorf("could not write to bucket %q: %w", bucket, err)
	}
	if err := c.Delete(ctx, bucket, key); err != nil {
		return fmt.Errorf("could write but not delete in bucket %q: %w", bucket, err)
	}
	return nil
}

// readBackup fetches a sealed dump from wherever it was stored.
func (a *App) readBackup(ctx context.Context, b *store.Backup) ([]byte, error) {
	if b.Destination != "s3" {
		return os.ReadFile(filepath.Join(a.backupDir(), *b.ObjectKey))
	}
	if b.ViaResourceID == nil {
		return nil, errors.New("backup has no backup service")
	}
	via, err := a.Store.GetResource(ctx, *b.ViaResourceID)
	if err != nil {
		return nil, err
	}
	c, bucket, _, err := a.backupS3(a.BackupSettingsOf(via))
	if err != nil {
		return nil, err
	}
	res, err := c.Get(ctx, bucket, *b.ObjectKey, "")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

// RunBackupService backs up every target of a backup service now.
func (a *App) RunBackupService(ctx context.Context, r *store.Resource) (int, error) {
	bs := a.BackupSettingsOf(r)
	n := 0
	for _, name := range bs.Targets {
		var target *store.Resource
		for _, kind := range []string{"postgres", "vector", "queue", "auth"} {
			if t, err := a.Store.GetResourceByName(ctx, r.ProjectID, kind, name); err == nil {
				target = t
				break
			}
		}
		if target == nil {
			a.svcLog(ctx, r, "backup", "warn", "skipped "+name+": not a database service in this project", nil)
			continue
		}
		b, err := a.Store.CreateBackupVia(ctx, target.ID, r.ID, bs.Destination.Type)
		if err != nil {
			return n, err
		}
		if err := a.Enqueue(ctx, "backup_resource", map[string]string{"resourceId": target.ID, "backupId": b.ID}, "backup:"+b.ID); err != nil {
			return n, err
		}
		a.svcLog(ctx, r, "backup", "info", "backing up "+name, map[string]any{"backup": b.ID, "destination": bs.Destination.Type})
		n++
	}
	return n, nil
}

// BackupTick starts scheduled runs of backup services.
func (a *App) BackupTick(ctx context.Context) error {
	list, err := a.Store.ResourcesByKind(ctx, "backup", "active")
	if err != nil {
		return err
	}
	for i := range list {
		r := &list[i]
		bs := a.BackupSettingsOf(r)
		sched, err := cronexpr.Parse(bs.Schedule)
		if err != nil {
			continue
		}
		now := a.Now()
		if bs.NextRunAt == nil {
			next := sched.Next(now)
			bs.NextRunAt = &next
			_ = a.Store.SetResourceSettings(ctx, r.ID, bs)
			continue
		}
		if bs.NextRunAt.After(now) {
			continue
		}
		next := sched.Next(now)
		bs.NextRunAt = &next
		if err := a.Store.SetResourceSettings(ctx, r.ID, bs); err != nil {
			return err
		}
		if _, err := a.RunBackupService(ctx, r); err != nil {
			a.svcLog(ctx, r, "backup", "error", "scheduled run failed", map[string]any{"error": err.Error()})
		}
	}
	return nil
}

func decodeB64(s string) ([]byte, error) { return base64Std.DecodeString(s) }

var base64Std = b64.StdEncoding
