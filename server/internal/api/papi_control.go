package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/app"
	"github.com/shellhaki/sparkbase/server/internal/cronexpr"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/netguard"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

var itemNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _.-]{0,62}$`)

func (s *Server) cronRoutes() {
	base := "/p/{ref}/cron/{name}"
	load := func(r *http.Request, pc *pctx) (*store.Resource, error) {
		return s.service(r, pc, "cron", r.PathValue("name"))
	}
	jobOut := func(j store.CronJob) map[string]any {
		var act any
		_ = json.Unmarshal(j.Action, &act)
		return map[string]any{"id": j.ID, "name": j.Name, "schedule": j.Schedule, "action": act, "enabled": j.Enabled,
			"nextRunAt": j.NextRunAt, "lastRunAt": j.LastRunAt, "lastStatus": j.LastStatus, "createdAt": j.CreatedAt}
	}
	parseSchedule := func(res *store.Resource, schedule string) (*cronexpr.Schedule, error) {
		sch, err := cronexpr.Parse(schedule)
		if err != nil {
			return nil, httpx.Invalid(err.Error())
		}
		if min := res.Limits.Get("min_interval_seconds"); min > 0 && sch.MinInterval(time.Now()) < time.Duration(min)*time.Second {
			return nil, httpx.Invalid(fmt.Sprintf("this plan runs jobs at most every %d seconds", min))
		}
		return sch, nil
	}

	s.papi("GET "+base+"/jobs", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		list, err := s.Store.ListCronJobs(r.Context(), res.ID)
		if err != nil {
			return err
		}
		out := make([]map[string]any, len(list))
		for i, j := range list {
			out[i] = jobOut(j)
		}
		return httpx.OK(w, map[string]any{"jobs": out})
	})

	s.papi("POST "+base+"/jobs", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Name     string          `json:"name"`
			Schedule string          `json:"schedule"`
			Action   app.CronAction  `json:"action"`
			Raw      json.RawMessage `json:"-"`
		}
		if err := httpx.Decode(r, &in, 64<<10); err != nil {
			return err
		}
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		if !itemNameRe.MatchString(in.Name) {
			return httpx.Invalid("name must be 1 to 63 letters, digits, spaces, dots, dashes or underscores")
		}
		sch, err := parseSchedule(res, in.Schedule)
		if err != nil {
			return err
		}
		if err := app.ValidateCronAction(in.Action); err != nil {
			return httpx.Invalid(err.Error())
		}
		ctx := r.Context()
		if max := res.Limits.Get("max_jobs"); max > 0 {
			if n, _ := s.Store.CountCronJobs(ctx, res.ID); int64(n) >= max {
				return httpx.Quota(fmt.Sprintf("this plan allows %d jobs", max))
			}
		}
		act, _ := json.Marshal(in.Action)
		j, err := s.Store.CreateCronJob(ctx, res.ID, in.Name, in.Schedule, act, sch.Next(time.Now()))
		if errors.Is(err, store.ErrConflict) {
			return httpx.Conflict("a job named " + in.Name + " exists")
		}
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, 201, map[string]any{"job": jobOut(*j)})
	})

	s.papi("PATCH "+base+"/jobs/{id}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Schedule *string         `json:"schedule"`
			Action   *app.CronAction `json:"action"`
			Enabled  *bool           `json:"enabled"`
		}
		if err := httpx.Decode(r, &in, 64<<10); err != nil {
			return err
		}
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		j, err := s.Store.GetCronJob(ctx, res.ID, r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("job")
		}
		if err != nil {
			return err
		}
		var next *time.Time
		if in.Schedule != nil {
			sch, err := parseSchedule(res, *in.Schedule)
			if err != nil {
				return err
			}
			n := sch.Next(time.Now())
			next = &n
		}
		var act json.RawMessage
		if in.Action != nil {
			if err := app.ValidateCronAction(*in.Action); err != nil {
				return httpx.Invalid(err.Error())
			}
			act, _ = json.Marshal(in.Action)
		}
		if err := s.Store.UpdateCronJob(ctx, j.ID, in.Schedule, act, in.Enabled, next); err != nil {
			return err
		}
		j, err = s.Store.GetCronJob(ctx, res.ID, j.ID)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"job": jobOut(*j)})
	})

	s.papi("DELETE "+base+"/jobs/{id}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		ok, err := s.Store.DeleteCronJob(r.Context(), res.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if !ok {
			return httpx.NotFound("job")
		}
		return httpx.NoContent(w)
	})

	s.papi("POST "+base+"/jobs/{id}/run", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		j, err := s.Store.GetCronJob(r.Context(), res.ID, r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("job")
		}
		if err != nil {
			return err
		}
		status, output := s.RunCronJob(r.Context(), *j)
		return httpx.OK(w, map[string]any{"status": status, "output": output})
	})

	s.papi("GET "+base+"/jobs/{id}/runs", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		j, err := s.Store.GetCronJob(r.Context(), res.ID, r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("job")
		}
		if err != nil {
			return err
		}
		runs, err := s.Store.ListCronRuns(r.Context(), j.ID, queryInt(r, "limit", 50, 1, 500))
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"runs": runs})
	})
}

func (s *Server) monitorRoutes() {
	base := "/p/{ref}/monitor/{name}"
	load := func(r *http.Request, pc *pctx) (*store.Resource, error) {
		return s.service(r, pc, "monitor", r.PathValue("name"))
	}
	withUptime := func(r *http.Request, m store.Monitor) map[string]any {
		up24, _ := s.Store.Uptime(r.Context(), m.ID, time.Now().Add(-24*time.Hour))
		up7, _ := s.Store.Uptime(r.Context(), m.ID, time.Now().Add(-7*24*time.Hour))
		recent, _ := s.Store.MonitorResults(r.Context(), m.ID, time.Now().Add(-24*time.Hour), 60)
		return map[string]any{"check": m, "uptime24h": up24, "uptime7d": up7, "recent": recent}
	}

	s.papi("GET "+base+"/checks", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		list, err := s.Store.ListMonitors(r.Context(), res.ID)
		if err != nil {
			return err
		}
		out := make([]map[string]any, len(list))
		for i, m := range list {
			out[i] = withUptime(r, m)
		}
		var ms app.MonitorSettings
		_ = json.Unmarshal(res.Settings, &ms)
		return httpx.OK(w, map[string]any{"checks": out, "settings": ms})
	})

	s.papi("POST "+base+"/checks", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Name            string `json:"name"`
			URL             string `json:"url"`
			Method          string `json:"method"`
			IntervalSeconds int    `json:"intervalSeconds"`
			TimeoutSeconds  int    `json:"timeoutSeconds"`
			ExpectStatus    int    `json:"expectStatus"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		if !itemNameRe.MatchString(in.Name) {
			return httpx.Invalid("name must be 1 to 63 letters, digits, spaces, dots, dashes or underscores")
		}
		if err := netguard.CheckURL(in.URL); err != nil {
			return httpx.Invalid(err.Error())
		}
		if in.Method == "" {
			in.Method = "GET"
		}
		in.Method = strings.ToUpper(in.Method)
		if in.Method != "GET" && in.Method != "HEAD" && in.Method != "POST" {
			return httpx.Invalid("method must be GET, HEAD or POST")
		}
		if in.IntervalSeconds == 0 {
			in.IntervalSeconds = 60
		}
		if min := res.Limits.Get("min_interval_seconds"); min > 0 && int64(in.IntervalSeconds) < min {
			return httpx.Invalid(fmt.Sprintf("this plan checks at most every %d seconds", min))
		}
		if in.TimeoutSeconds <= 0 || in.TimeoutSeconds > 60 {
			in.TimeoutSeconds = 10
		}
		if in.ExpectStatus == 0 {
			in.ExpectStatus = 200
		}
		ctx := r.Context()
		if max := res.Limits.Get("max_checks"); max > 0 {
			if n, _ := s.Store.CountMonitors(ctx, res.ID); int64(n) >= max {
				return httpx.Quota(fmt.Sprintf("this plan allows %d checks", max))
			}
		}
		m, err := s.Store.CreateMonitor(ctx, store.Monitor{ResourceID: res.ID, Name: in.Name, URL: in.URL, Method: in.Method,
			IntervalSeconds: in.IntervalSeconds, TimeoutSeconds: in.TimeoutSeconds, ExpectStatus: in.ExpectStatus})
		if errors.Is(err, store.ErrConflict) {
			return httpx.Conflict("a check named " + in.Name + " exists")
		}
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, 201, map[string]any{"check": m})
	})

	s.papi("PATCH "+base+"/checks/{id}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Enabled         *bool   `json:"enabled"`
			IntervalSeconds *int    `json:"intervalSeconds"`
			URL             *string `json:"url"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		if in.URL != nil {
			if err := netguard.CheckURL(*in.URL); err != nil {
				return httpx.Invalid(err.Error())
			}
		}
		if in.IntervalSeconds != nil {
			if min := res.Limits.Get("min_interval_seconds"); min > 0 && int64(*in.IntervalSeconds) < min {
				return httpx.Invalid(fmt.Sprintf("this plan checks at most every %d seconds", min))
			}
		}
		m, err := s.Store.GetMonitor(r.Context(), res.ID, r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("check")
		}
		if err != nil {
			return err
		}
		if err := s.Store.UpdateMonitor(r.Context(), m.ID, in.Enabled, in.IntervalSeconds, in.URL); err != nil {
			return err
		}
		m, _ = s.Store.GetMonitor(r.Context(), res.ID, m.ID)
		return httpx.OK(w, map[string]any{"check": m})
	})

	s.papi("DELETE "+base+"/checks/{id}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		ok, err := s.Store.DeleteMonitor(r.Context(), res.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if !ok {
			return httpx.NotFound("check")
		}
		return httpx.NoContent(w)
	})

	s.papi("POST "+base+"/checks/{id}/run", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		m, err := s.Store.GetMonitor(r.Context(), res.ID, r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("check")
		}
		if err != nil {
			return err
		}
		st, _ := s.Settings(r.Context())
		return httpx.OK(w, map[string]any{"result": s.RunCheck(r.Context(), *m, st.AllowPrivateTargets)})
	})

	s.papi("GET "+base+"/checks/{id}/results", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		m, err := s.Store.GetMonitor(r.Context(), res.ID, r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			return httpx.NotFound("check")
		}
		if err != nil {
			return err
		}
		hours := queryInt(r, "hours", 24, 1, 24*7)
		list, err := s.Store.MonitorResults(r.Context(), m.ID, time.Now().Add(-time.Duration(hours)*time.Hour), 2000)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"results": list})
	})

	s.papi("PATCH "+base+"/settings", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in app.MonitorSettings
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if in.AlertWebhook != "" {
			if err := netguard.CheckURL(in.AlertWebhook); err != nil {
				return httpx.Invalid(err.Error())
			}
		}
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		if err := s.Store.SetResourceSettings(r.Context(), res.ID, in); err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"settings": in})
	})
}

func (s *Server) backupRoutes() {
	base := "/p/{ref}/backup/{name}"
	load := func(r *http.Request, pc *pctx) (*store.Resource, error) {
		return s.service(r, pc, "backup", r.PathValue("name"))
	}
	// The S3 secret is never returned.
	view := func(bs app.BackupSettings) map[string]any {
		d := bs.Destination
		hasSecret := d.SecretEnc != ""
		d.SecretEnc = ""
		if d.Provider == "r2" {
			d.Type = "r2"
		}
		return map[string]any{"schedule": bs.Schedule, "retentionDays": bs.RetentionDays, "targets": bs.Targets,
			"destination": d, "hasSecret": hasSecret, "nextRunAt": bs.NextRunAt}
	}

	s.papi("GET "+base+"/settings", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"settings": view(s.BackupSettingsOf(res))})
	})

	s.papi("PATCH "+base+"/settings", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Schedule      *string  `json:"schedule"`
			RetentionDays *int     `json:"retentionDays"`
			Targets       []string `json:"targets"`
			Destination   *struct {
				Type         string `json:"type"`
				AccountID    string `json:"accountId"`
				Jurisdiction string `json:"jurisdiction"`
				Endpoint     string `json:"endpoint"`
				Bucket       string `json:"bucket"`
				Region       string `json:"region"`
				Prefix       string `json:"prefix"`
				AccessKey    string `json:"accessKey"`
				SecretKey    string `json:"secretKey"`
			} `json:"destination"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		bs := s.BackupSettingsOf(res)
		if in.Schedule != nil {
			sch, err := cronexpr.Parse(*in.Schedule)
			if err != nil {
				return httpx.Invalid(err.Error())
			}
			if sch.MinInterval(time.Now()) < time.Hour {
				return httpx.Invalid("backups run at most once an hour")
			}
			bs.Schedule = *in.Schedule
			next := sch.Next(time.Now())
			bs.NextRunAt = &next
		}
		if in.RetentionDays != nil {
			max := res.Limits.Get("retention_days")
			if *in.RetentionDays < 1 || (max > 0 && int64(*in.RetentionDays) > max) {
				return httpx.Invalid(fmt.Sprintf("retentionDays must be 1 to %d on this plan", max))
			}
			bs.RetentionDays = *in.RetentionDays
		}
		if in.Targets != nil {
			if max := res.Limits.Get("max_targets"); max > 0 && int64(len(in.Targets)) > max {
				return httpx.Invalid(fmt.Sprintf("this plan backs up at most %d services", max))
			}
			bs.Targets = in.Targets
		}
		if d := in.Destination; d != nil {
			switch d.Type {
			case "local":
				bs.Destination = app.BackupDestination{Type: "local"}
			case "s3", "r2":
				nd := app.BackupDestination{Type: "s3", Endpoint: d.Endpoint, Bucket: d.Bucket, Region: d.Region, Prefix: d.Prefix, AccessKey: d.AccessKey}
				if d.Type == "r2" {
					ep, err := app.R2Endpoint(strings.ToLower(strings.TrimSpace(d.AccountID)), d.Jurisdiction)
					if err != nil {
						return httpx.Invalid(err.Error())
					}
					nd.Provider, nd.AccountID, nd.Jurisdiction, nd.Endpoint, nd.Region = "r2", strings.ToLower(strings.TrimSpace(d.AccountID)), d.Jurisdiction, ep, "auto"
				}
				if err := netguard.CheckURL(nd.Endpoint); err != nil || nd.Bucket == "" || nd.AccessKey == "" {
					return httpx.Invalid("bucket destinations need an endpoint, a bucket and an access key")
				}
				// Keep the stored secret when the same bucket is saved again without one.
				old := bs.Destination
				if d.SecretKey != "" {
					nd.SecretEnc = base64.StdEncoding.EncodeToString(s.Box.SealString(d.SecretKey))
				} else if old.Type == "s3" && old.Endpoint == nd.Endpoint && old.AccessKey == nd.AccessKey {
					nd.SecretEnc = old.SecretEnc
				}
				if nd.SecretEnc == "" {
					return httpx.Invalid("secretKey is required")
				}
				bs.Destination = nd
				if err := s.CheckBackupDestination(r.Context(), bs); err != nil {
					return httpx.Invalid(err.Error())
				}
			default:
				return httpx.Invalid("destination type must be local, s3 or r2")
			}
		}
		if err := s.Store.SetResourceSettings(r.Context(), res.ID, bs); err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"settings": view(bs)})
	})

	s.papi("POST "+base+"/run", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		n, err := s.RunBackupService(r.Context(), res)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, 202, map[string]any{"started": n})
	})

	s.papi("GET "+base+"/runs", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := load(r, pc)
		if err != nil {
			return err
		}
		list, err := s.Store.ListBackupsVia(r.Context(), res.ID, queryInt(r, "limit", 100, 1, 500))
		if err != nil {
			return err
		}
		names := map[string]string{}
		out := make([]map[string]any, len(list))
		for i, b := range list {
			if _, ok := names[b.ResourceID]; !ok {
				if t, err := s.Store.GetResource(r.Context(), b.ResourceID); err == nil {
					names[b.ResourceID] = t.Name
				}
			}
			out[i] = map[string]any{"backup": b, "service": names[b.ResourceID]}
		}
		return httpx.OK(w, map[string]any{"runs": out})
	})
}
