package drivers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Older Redis versions lack some commands we deny (WAITAOF arrived in 7.2).
// Rules naming unknown commands are dropped instead of failing provisioning.
func TestSetUserRulesSkipsUnknownCommands(t *testing.T) {
	ctx := context.Background()
	rc := AdminClient("127.0.0.1:63799", "devredis")
	defer rc.Close()
	if err := rc.Ping(ctx).Err(); err != nil {
		if os.Getenv("SB_REQUIRE_SERVICES") != "" {
			t.Fatalf("redis: %v", err)
		}
		t.Skip("dev redis not running")
	}
	user := "sbtest_unknown_rules"
	defer rc.Do(ctx, "acl", "deluser", user)

	rules := append(cacheRules("sbtest:", "pw-for-test-only-123", true, false), "-sb_no_such_command", "+client|sb_no_such_sub")
	if err := setUserRules(ctx, rc, user, rules); err != nil {
		t.Fatalf("setUserRules: %v", err)
	}
	got, err := rc.Do(ctx, "acl", "getuser", user).Result()
	if err != nil {
		t.Fatal(err)
	}
	// The known denials are still in place.
	text := fmt.Sprint(got)
	for _, want := range []string{"-keys", "-flushall", "-acl"} {
		if !strings.Contains(text, want) {
			t.Fatalf("%s missing from %s", want, text)
		}
	}
	// A category typo is not silently dropped.
	if err := setUserRules(ctx, rc, user, []any{"reset", "-@no_such_category"}); err == nil {
		t.Fatal("unknown category was accepted")
	}
}
