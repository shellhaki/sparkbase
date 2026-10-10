package drivers

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
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
	// The known denials still hold: the user cannot list or wipe keys.
	uc := redis.NewClient(&redis.Options{Addr: "127.0.0.1:63799", Username: user, Password: "pw-for-test-only-123", DisableIdentity: true})
	defer uc.Close()
	for _, cmd := range [][]any{{"keys", "*"}, {"flushall"}, {"acl", "list"}} {
		if err := uc.Do(ctx, cmd...).Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
			t.Fatalf("%v was not refused: %v", cmd, err)
		}
	}
	if err := uc.Set(ctx, "sbtest:k", "v", 0).Err(); err != nil {
		t.Fatalf("own prefix write refused: %v", err)
	}
	uc.Del(ctx, "sbtest:k")
	// A category typo is not silently dropped.
	if err := setUserRules(ctx, rc, user, []any{"reset", "-@no_such_category"}); err == nil {
		t.Fatal("unknown category was accepted")
	}
}
