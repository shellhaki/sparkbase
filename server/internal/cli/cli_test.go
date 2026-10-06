package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/shellhaki/sparkbase/server/internal/cli"
	"github.com/shellhaki/sparkbase/server/internal/testenv"
)

func TestMergeEnv(t *testing.T) {
	in := "# my app\nPORT=3000\nexport DATABASE_URL=old\n\nSECRET='x'\n"
	out := cli.MergeEnv(in, map[string]string{"DATABASE_URL": "postgres://u:p@h/db", "SPARKBASE_URL": "http://x/p/abc", "WEIRD": "a b"})
	want := "# my app\nPORT=3000\nDATABASE_URL=postgres://u:p@h/db\n\nSECRET='x'\n# Sparkbase\nSPARKBASE_URL=http://x/p/abc\nWEIRD=\"a b\"\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	if again := cli.MergeEnv(out, map[string]string{"SPARKBASE_URL": "http://y"}); strings.Count(again, "SPARKBASE_URL") != 1 {
		t.Fatalf("duplicated key:\n%s", again)
	}
}

func TestEnvName(t *testing.T) {
	if cli.EnvName("main-db") != "MAIN_DB" || cli.EnvName("x") != "X" {
		t.Fatal(cli.EnvName("main-db"))
	}
}

func TestCLIEndToEnd(t *testing.T) {
	e := testenv.New(t)
	e.Admin()
	dir := t.TempDir()
	var out bytes.Buffer
	c := cli.New()
	c.Out, c.Err = &out, &out
	c.ConfigPath = filepath.Join(dir, "config.json")
	c.LinkDir = dir
	run := func(args ...string) string {
		t.Helper()
		out.Reset()
		if err := c.Run(args); err != nil {
			t.Fatalf("sparkbase %s: %v\n%s", strings.Join(args, " "), err, out.String())
		}
		return out.String()
	}

	t.Setenv("SPARKBASE_PASSWORD", testenv.Password)
	if s := run("login", "--url", e.URL, "--email", "admin@example.com"); !strings.Contains(s, "Logged in") {
		t.Fatal(s)
	}
	created := run("projects", "create", "My App")
	ref := strings.Fields(created[strings.Index(created, "(ref ")+5:])[0]
	ref = strings.TrimSuffix(ref, ")")
	run("link", ref)
	run("services", "add", "postgres", "main")
	run("services", "add", "cache", "sessions", "--plan", "cache-medium")
	list := run("services", "list")
	if !strings.Contains(list, "sessions") || !strings.Contains(list, "cache-medium") {
		t.Fatalf("list: %s", list)
	}
	os.WriteFile(filepath.Join(dir, ".env"), []byte("PORT=3000\n"), 0o600)
	run("env", "pull")
	env, _ := os.ReadFile(filepath.Join(dir, ".env"))
	vars := map[string]string{}
	for _, line := range strings.Split(string(env), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			vars[k] = strings.Trim(v, `"`)
		}
	}
	for _, k := range []string{"PORT", "DATABASE_URL", "MAIN_DATABASE_URL", "REDIS_URL", "SPARKBASE_URL", "SPARKBASE_SECRET_KEY", "SPARKBASE_PUBLISHABLE_KEY"} {
		if vars[k] == "" {
			t.Errorf("missing %s in .env:\n%s", k, env)
		}
	}
	conn, err := pgx.Connect(context.Background(), vars["DATABASE_URL"])
	if err != nil {
		t.Fatalf("DATABASE_URL from env pull does not connect: %v", err)
	}
	conn.Close(context.Background())
	run("usage")
	run("admin", "users")
	if s := run("admin", "invite", "new@example.com"); !strings.Contains(s, "/invite?token=") {
		t.Fatal(s)
	}
	run("services", "pause", "main")
	run("services", "delete", "sessions")
}
