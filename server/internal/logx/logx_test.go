package logx

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "info")
	log.Info("connect",
		"password", "hunter2",
		"api_key", "sb_abcdefgh12345678",
		"email", "ada@example.com",
		"target", "postgres://role:s3cret@db.local:5432/x",
		"note", "used sbs_ABCDEFGHijklmnop1234 here",
	)
	out := buf.String()
	for _, leak := range []string{"hunter2", "sb_abcdefgh12345678", "ada@example.com", "s3cret", "sbs_ABCDEFGHijklmnop1234"} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaked %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, "a***@example.com") {
		t.Errorf("email not masked: %s", out)
	}
}
