// Package cli implements the sparkbase command line client.
package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// Config is stored in ~/.config/sparkbase/config.json.
type Config struct {
	URL string `json:"url"`
	Key string `json:"key"`
}

// Link is stored in .sparkbase/project.json in a project folder.
type Link struct {
	ProjectID string `json:"projectId"`
	Ref       string `json:"ref"`
}

type CLI struct {
	Out, Err io.Writer
	In       io.Reader
	// ConfigPath and LinkDir default to the user config dir and the working dir.
	ConfigPath string
	LinkDir    string
	HTTP       *http.Client
	cfg        *Config
}

func New() *CLI {
	dir, _ := os.UserConfigDir()
	wd, _ := os.Getwd()
	return &CLI{Out: os.Stdout, Err: os.Stderr, In: os.Stdin,
		ConfigPath: filepath.Join(dir, "sparkbase", "config.json"), LinkDir: wd,
		HTTP: &http.Client{Timeout: 60 * time.Second}}
}

const usage = `sparkbase: the command line for a Sparkbase install

Usage:
  sparkbase login --url URL [--email EMAIL] [--key sb_...]
  sparkbase logout
  sparkbase projects list
  sparkbase projects create NAME
  sparkbase link REF                       link this folder to a project
  sparkbase services list
  sparkbase services add KIND NAME [--plan PLAN]
  sparkbase services delete NAME
  sparkbase services pause NAME | resume NAME
  sparkbase env pull [--file .env]         write the project URL, keys and connection strings
  sparkbase usage
  sparkbase admin invite EMAIL [--role member|admin]
  sparkbase admin users
  sparkbase admin hosts

Kinds: postgres, cache, storage, vector, queue, auth
`

// Run executes one command and returns an error to print.
func (c *CLI) Run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(c.Out, usage)
		return nil
	}
	switch args[0] {
	case "login":
		return c.login(args[1:])
	case "logout":
		return os.Remove(c.ConfigPath)
	case "projects":
		return c.projects(args[1:])
	case "link":
		return c.link(args[1:])
	case "services":
		return c.services(args[1:])
	case "env":
		if len(args) < 2 || args[1] != "pull" {
			return errors.New("usage: sparkbase env pull [--file .env]")
		}
		return c.envPull(args[2:])
	case "usage":
		return c.usage()
	case "admin":
		return c.admin(args[1:])
	}
	return fmt.Errorf("unknown command %q; run sparkbase help", args[0])
}

func (c *CLI) loadConfig() (*Config, error) {
	if c.cfg != nil {
		return c.cfg, nil
	}
	b, err := os.ReadFile(c.ConfigPath)
	if err != nil {
		return nil, errors.New("not logged in; run sparkbase login --url https://api.example.com")
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	c.cfg = &cfg
	return &cfg, nil
}

func (c *CLI) saveConfig(cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(c.ConfigPath), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(c.ConfigPath, b, 0o600)
}

func (c *CLI) loadLink() (*Link, error) {
	b, err := os.ReadFile(filepath.Join(c.LinkDir, ".sparkbase", "project.json"))
	if err != nil {
		return nil, errors.New("this folder is not linked to a project; run sparkbase link REF")
	}
	var l Link
	return &l, json.Unmarshal(b, &l)
}

// APIError is an error response from the server.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("%s (%s)", e.Message, e.Code) }

func (c *CLI) call(method, path string, body any, out any) error {
	cfg, err := c.loadConfig()
	if err != nil {
		return err
	}
	return c.do(c.HTTP, cfg.URL, cfg.Key, method, path, body, out)
}

func (c *CLI) do(client *http.Client, base, key, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(base, "/")+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("X-Sparkbase-Client", "cli")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		var e struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error.Code == "" {
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
		}
		return &APIError{Status: res.StatusCode, Code: e.Error.Code, Message: e.Error.Message}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *CLI) prompt(label string) string {
	fmt.Fprint(c.Err, label)
	line, _ := bufio.NewReader(c.In).ReadString('\n')
	return strings.TrimSpace(line)
}

func (c *CLI) login(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	url := fs.String("url", "", "API URL, e.g. https://api.example.com")
	email := fs.String("email", "", "account email")
	key := fs.String("key", "", "an existing account key (sb_...)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" {
		return errors.New("--url is required")
	}
	base := strings.TrimRight(*url, "/")
	if *key == "" {
		if *email == "" {
			*email = c.prompt("Email: ")
		}
		password := os.Getenv("SPARKBASE_PASSWORD")
		if password == "" {
			password = c.readPassword("Password: ")
		}
		jar, _ := cookiejar.New(nil)
		session := &http.Client{Jar: jar, Timeout: 30 * time.Second}
		if err := c.do(session, base, "", "POST", "/v1/auth/login", map[string]string{"email": *email, "password": password}, nil); err != nil {
			return err
		}
		host, _ := os.Hostname()
		var created struct {
			Secret string `json:"secret"`
		}
		if err := c.do(session, base, "", "POST", "/v1/api-keys", map[string]any{"name": "cli " + host}, &created); err != nil {
			return err
		}
		*key = created.Secret
	}
	var me struct {
		User struct{ Email, Role string } `json:"user"`
	}
	if err := c.do(c.HTTP, base, *key, "GET", "/v1/me", nil, &me); err != nil {
		return err
	}
	if err := c.saveConfig(&Config{URL: base, Key: *key}); err != nil {
		return err
	}
	fmt.Fprintf(c.Out, "Logged in to %s as %s (%s)\n", base, me.User.Email, me.User.Role)
	return nil
}

type project struct {
	ID   string `json:"id"`
	Ref  string `json:"ref"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type service struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Name        string         `json:"name"`
	Plan        string         `json:"plan"`
	Status      string         `json:"status"`
	WriteLocked bool           `json:"writeLocked"`
	APIURL      string         `json:"apiUrl"`
	LastError   *string        `json:"lastError"`
	Limits      map[string]any `json:"limits"`
}

func (c *CLI) table() *tabwriter.Writer { return tabwriter.NewWriter(c.Out, 0, 4, 2, ' ', 0) }

func (c *CLI) projects(args []string) error {
	if len(args) == 0 || args[0] == "list" {
		var out struct{ Projects []project }
		if err := c.call("GET", "/v1/projects", nil, &out); err != nil {
			return err
		}
		w := c.table()
		fmt.Fprintln(w, "REF\tNAME\tURL")
		for _, p := range out.Projects {
			fmt.Fprintf(w, "%s\t%s\t%s\n", p.Ref, p.Name, p.URL)
		}
		return w.Flush()
	}
	if args[0] == "create" && len(args) >= 2 {
		var out struct{ Project project }
		if err := c.call("POST", "/v1/projects", map[string]string{"name": strings.Join(args[1:], " ")}, &out); err != nil {
			return err
		}
		fmt.Fprintf(c.Out, "Created %s (ref %s)\nProject URL: %s\nLink this folder: sparkbase link %s\n", out.Project.Name, out.Project.Ref, out.Project.URL, out.Project.Ref)
		return nil
	}
	return errors.New("usage: sparkbase projects list | create NAME")
}

func (c *CLI) findProject(ref string) (*project, error) {
	var out struct{ Projects []project }
	if err := c.call("GET", "/v1/projects", nil, &out); err != nil {
		return nil, err
	}
	for _, p := range out.Projects {
		if p.Ref == ref || p.ID == ref {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("no project %q; see sparkbase projects list", ref)
}

func (c *CLI) link(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: sparkbase link REF")
	}
	p, err := c.findProject(args[0])
	if err != nil {
		return err
	}
	dir := filepath.Join(c.LinkDir, ".sparkbase")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(Link{ProjectID: p.ID, Ref: p.Ref}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "project.json"), b, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(c.Out, "Linked to %s (%s)\n", p.Name, p.Ref)
	return nil
}

func (c *CLI) listServices(projectID string) ([]service, error) {
	var out struct{ Resources []service }
	err := c.call("GET", "/v1/projects/"+projectID+"/resources", nil, &out)
	return out.Resources, err
}

func (c *CLI) serviceByName(projectID, name string) (*service, error) {
	list, err := c.listServices(projectID)
	if err != nil {
		return nil, err
	}
	for _, s := range list {
		if s.Name == name {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("no service named %q in this project", name)
}

func (c *CLI) services(args []string) error {
	l, err := c.loadLink()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "list" {
		list, err := c.listServices(l.ProjectID)
		if err != nil {
			return err
		}
		w := c.table()
		fmt.Fprintln(w, "NAME\tKIND\tPLAN\tSTATUS\tAPI URL")
		for _, s := range list {
			status := s.Status
			if s.WriteLocked {
				status += " (read-only)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Kind, s.Plan, status, s.APIURL)
		}
		return w.Flush()
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		plan := fs.String("plan", "", "plan id, e.g. postgres-medium")
		rest := reorder(args[1:], "plan")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() != 2 {
			return errors.New("usage: sparkbase services add KIND NAME [--plan PLAN]")
		}
		var out struct{ Resource service }
		body := map[string]string{"kind": fs.Arg(0), "name": fs.Arg(1), "plan": *plan}
		if err := c.call("POST", "/v1/projects/"+l.ProjectID+"/resources", body, &out); err != nil {
			return err
		}
		fmt.Fprintf(c.Out, "Creating %s %s", out.Resource.Kind, out.Resource.Name)
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			var cur struct{ Resource service }
			if err := c.call("GET", "/v1/resources/"+out.Resource.ID, nil, &cur); err != nil {
				return err
			}
			switch cur.Resource.Status {
			case "active":
				fmt.Fprintf(c.Out, " done\nAPI URL: %s\nRun sparkbase env pull to update .env\n", cur.Resource.APIURL)
				return nil
			case "failed":
				msg := ""
				if cur.Resource.LastError != nil {
					msg = *cur.Resource.LastError
				}
				return fmt.Errorf("\nprovisioning failed: %s", msg)
			}
			fmt.Fprint(c.Out, ".")
			time.Sleep(time.Second)
		}
		return errors.New("\nstill provisioning; check sparkbase services list")
	case "delete", "pause", "resume":
		if len(args) != 2 {
			return fmt.Errorf("usage: sparkbase services %s NAME", args[0])
		}
		s, err := c.serviceByName(l.ProjectID, args[1])
		if err != nil {
			return err
		}
		if args[0] == "delete" {
			err = c.call("DELETE", "/v1/resources/"+s.ID, nil, nil)
		} else {
			err = c.call("POST", "/v1/resources/"+s.ID+"/"+args[0], nil, nil)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(c.Out, "%s: %s\n", args[0], s.Name)
		return nil
	}
	return errors.New("usage: sparkbase services list | add | delete | pause | resume")
}

// reorder moves --flag value pairs before positional arguments so the
// standard flag package can parse "add postgres main --plan x".
func reorder(args []string, flags ...string) []string {
	var f, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		matched := false
		for _, name := range flags {
			if a == "--"+name || a == "-"+name {
				if i+1 < len(args) {
					f = append(f, a, args[i+1])
					i++
				}
				matched = true
			} else if strings.HasPrefix(a, "--"+name+"=") {
				f = append(f, a)
				matched = true
			}
		}
		if !matched {
			pos = append(pos, a)
		}
	}
	return append(f, pos...)
}

var nonIdent = regexp.MustCompile(`[^A-Z0-9]+`)

// EnvName turns a service name into an environment variable prefix.
func EnvName(name string) string {
	return strings.Trim(nonIdent.ReplaceAllString(strings.ToUpper(name), "_"), "_")
}

func (c *CLI) envPull(args []string) error {
	fs := flag.NewFlagSet("env pull", flag.ContinueOnError)
	file := fs.String("file", ".env", "file to update")
	if err := fs.Parse(args); err != nil {
		return err
	}
	l, err := c.loadLink()
	if err != nil {
		return err
	}
	var keys struct {
		URL            string `json:"url"`
		PublishableKey string `json:"publishableKey"`
		SecretKey      string `json:"secretKey"`
	}
	if err := c.call("GET", "/v1/projects/"+l.ProjectID+"/keys", nil, &keys); err != nil {
		return err
	}
	vars := map[string]string{
		"SPARKBASE_URL":             keys.URL,
		"SPARKBASE_PUBLISHABLE_KEY": keys.PublishableKey,
		"SPARKBASE_SECRET_KEY":      keys.SecretKey,
	}
	list, err := c.listServices(l.ProjectID)
	if err != nil {
		return err
	}
	count := map[string]int{}
	for _, s := range list {
		count[s.Kind]++
	}
	for _, s := range list {
		if s.Status != "active" && s.Status != "paused" {
			continue
		}
		var conn struct {
			Native struct {
				URL       string `json:"url"`
				PooledURL string `json:"pooledUrl"`
				KeyPrefix string `json:"keyPrefix"`
			} `json:"native"`
			JWTSecret string `json:"jwtSecret"`
		}
		if err := c.call("GET", "/v1/resources/"+s.ID+"/connection", nil, &conn); err != nil {
			return err
		}
		n := EnvName(s.Name)
		switch s.Kind {
		case "postgres", "vector":
			suffix := "_DATABASE_URL"
			if s.Kind == "vector" {
				suffix = "_VECTOR_URL"
			}
			vars[n+suffix] = conn.Native.URL
			if s.Kind == "postgres" && count["postgres"] == 1 {
				vars["DATABASE_URL"] = conn.Native.URL
			}
		case "cache":
			vars[n+"_REDIS_URL"] = conn.Native.URL
			vars[n+"_REDIS_KEY_PREFIX"] = conn.Native.KeyPrefix
			if count["cache"] == 1 {
				vars["REDIS_URL"] = conn.Native.URL
				vars["REDIS_KEY_PREFIX"] = conn.Native.KeyPrefix
			}
		case "auth":
			vars["SPARKBASE_JWT_SECRET"] = conn.JWTSecret
		}
	}
	path := *file
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.LinkDir, path)
	}
	old, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(MergeEnv(string(old), vars)), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(c.Out, "Wrote %d variables to %s\n", len(vars), *file)
	return nil
}

// MergeEnv updates or appends KEY=value lines and leaves every other line
// (comments, unrelated variables, blank lines) as it was.
func MergeEnv(existing string, vars map[string]string) string {
	lines := strings.Split(existing, "\n")
	if existing == "" {
		lines = nil
	}
	seen := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		eq := strings.Index(trimmed, "=")
		if eq <= 0 || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k := strings.TrimSpace(trimmed[:eq])
		if v, ok := vars[k]; ok {
			lines[i] = k + "=" + quoteEnv(v)
			seen[k] = true
		}
	}
	var keys []string
	for k := range vars {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "# Sparkbase")
		for _, k := range keys {
			lines = append(lines, k+"="+quoteEnv(vars[k]))
		}
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func quoteEnv(v string) string {
	if strings.ContainsAny(v, " #\"'$`\\") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(v) + `"`
	}
	return v
}

func (c *CLI) usage() error {
	l, err := c.loadLink()
	if err != nil {
		return err
	}
	list, err := c.listServices(l.ProjectID)
	if err != nil {
		return err
	}
	w := c.table()
	fmt.Fprintln(w, "SERVICE\tMETRIC\tUSED\tLIMIT")
	for _, s := range list {
		var u struct {
			Usage  map[string]float64 `json:"usage"`
			Limits map[string]float64 `json:"limits"`
		}
		if err := c.call("GET", "/v1/resources/"+s.ID+"/usage", nil, &u); err != nil {
			continue
		}
		pairs := [][3]string{
			{"storage_bytes", "storage_mb", "MB"}, {"memory_bytes", "memory_mb", "MB"},
			{"connections", "direct_connections", ""}, {"users", "max_users", ""},
			{"requests_month", "requests_per_month", ""},
		}
		for _, p := range pairs {
			used, ok := u.Usage[p[0]]
			if !ok {
				continue
			}
			lim := "-"
			if v, ok := u.Limits[p[1]]; ok {
				if v < 0 {
					lim = "unlimited"
				} else {
					lim = fmt.Sprintf("%.0f%s", v, map[bool]string{true: " MB", false: ""}[p[2] == "MB"])
				}
			}
			val := fmt.Sprintf("%.0f", used)
			if p[2] == "MB" {
				val = fmt.Sprintf("%.1f MB", used/(1<<20))
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, p[0], val, lim)
		}
	}
	return w.Flush()
}

func (c *CLI) admin(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sparkbase admin invite EMAIL | users | hosts")
	}
	switch args[0] {
	case "invite":
		fs := flag.NewFlagSet("invite", flag.ContinueOnError)
		role := fs.String("role", "member", "member or admin")
		if err := fs.Parse(reorder(args[1:], "role")); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("usage: sparkbase admin invite EMAIL [--role member|admin]")
		}
		var out struct {
			Link    string `json:"link"`
			Emailed bool   `json:"emailed"`
		}
		if err := c.call("POST", "/v1/admin/invites", map[string]string{"email": fs.Arg(0), "role": *role}, &out); err != nil {
			return err
		}
		fmt.Fprintf(c.Out, "Invite link (valid 7 days): %s\n", out.Link)
		if out.Emailed {
			fmt.Fprintln(c.Out, "It was also emailed.")
		}
		return nil
	case "users":
		var out struct {
			Users []struct {
				Email      string  `json:"email"`
				Role       string  `json:"role"`
				DisabledAt *string `json:"disabledAt"`
			} `json:"users"`
		}
		if err := c.call("GET", "/v1/admin/users", nil, &out); err != nil {
			return err
		}
		w := c.table()
		fmt.Fprintln(w, "EMAIL\tROLE\tSTATUS")
		for _, u := range out.Users {
			st := "active"
			if u.DisabledAt != nil {
				st = "disabled"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", u.Email, u.Role, st)
		}
		return w.Flush()
	case "hosts":
		var out struct {
			Hosts []struct {
				Name          string `json:"name"`
				Kind          string `json:"kind"`
				Status        string `json:"status"`
				Weight        int    `json:"weight"`
				ResourceCount int    `json:"resourceCount"`
				InternalAddr  string `json:"internalAddr"`
			} `json:"hosts"`
		}
		if err := c.call("GET", "/v1/admin/hosts", nil, &out); err != nil {
			return err
		}
		w := c.table()
		fmt.Fprintln(w, "NAME\tKIND\tADDRESS\tSTATUS\tWEIGHT\tSERVICES")
		for _, h := range out.Hosts {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\n", h.Name, h.Kind, h.InternalAddr, h.Status, h.Weight, h.ResourceCount)
		}
		return w.Flush()
	}
	return errors.New("usage: sparkbase admin invite EMAIL | users | hosts")
}
