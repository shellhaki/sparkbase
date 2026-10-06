package drivers

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
)

// Cache gives each service one Redis ACL user restricted to its own key
// prefix on a shared Redis host.
type Cache struct{}

func NewCache() *Cache { return &Cache{} }

func (c *Cache) Kind() string     { return "cache" }
func (c *Cache) HostKind() string { return "cache" }

// KeyPrefix is the namespace every key of the service must start with.
func KeyPrefix(resourceID string) string { return "sb" + shortID(resourceID)[:16] + ":" }

func cacheUser(resourceID string) string { return "sbc_" + shortID(resourceID)[:16] }

// AdminClient connects to a cache host as its admin user.
func AdminClient(addr, secret string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr, Username: "default", Password: secret, PoolSize: 2, DisableIdentity: true})
}

func admin(t *Target) *redis.Client { return AdminClient(t.H.InternalAddr, t.AdminSecret) }

// Commands a tenant may never run. SCAN, KEYS and RANDOMKEY would reveal
// other tenants' key names, because ACL key patterns do not filter them.
var deniedCommands = []string{
	"keys", "scan", "randomkey", "flushall", "flushdb", "config", "debug", "script", "eval", "evalsha",
	"eval_ro", "evalsha_ro", "fcall", "fcall_ro", "function", "module", "acl", "shutdown", "migrate",
	"monitor", "sync", "psync", "replicaof", "slaveof", "save", "bgsave", "bgrewriteaof", "lastsave",
	"info", "dbsize", "swapdb", "select", "move", "object", "memory", "latency", "slowlog", "cluster",
	"failover", "client", "wait", "waitaof", "sort", "sort_ro", "copy",
}

// cacheRules returns the ACL rules for a user. Locked users can read and
// delete but not write.
func cacheRules(prefix, password string, enabled, locked bool) []any {
	rules := []any{"reset"}
	if enabled {
		rules = append(rules, "on")
	} else {
		rules = append(rules, "off")
	}
	rules = append(rules, ">"+password, "~"+prefix+"*", "resetchannels", "&"+prefix+"*", "+@all", "-@admin", "-@dangerous")
	for _, cmd := range deniedCommands {
		rules = append(rules, "-"+cmd)
	}
	// Harmless connection commands.
	rules = append(rules, "+client|setname", "+client|getname", "+client|id", "+client|info", "+client|setinfo")
	if locked {
		rules = append(rules, "-@write", "+del", "+unlink", "+expire", "+pexpire", "+persist")
	}
	return rules
}

func (c *Cache) setUser(ctx context.Context, rc *redis.Client, user, prefix, password string, enabled, locked bool) error {
	args := append([]any{"acl", "setuser", user}, cacheRules(prefix, password, enabled, locked)...)
	if err := rc.Do(ctx, args...).Err(); err != nil {
		return err
	}
	// Persist the ACL when the host uses an ACL file; ignore hosts without one.
	if err := rc.Do(ctx, "acl", "save").Err(); err != nil && !strings.Contains(err.Error(), "ACL file") && !strings.Contains(err.Error(), "aclfile") {
		return err
	}
	return nil
}

func (c *Cache) Provision(ctx context.Context, t *Target) (*Provisioned, error) {
	rc := admin(t)
	defer rc.Close()
	user, prefix := cacheUser(t.R.ID)+"_"+sbcrypto.RandomLower(4), KeyPrefix(t.R.ID)
	if existing, _ := t.State["user"].(string); existing != "" {
		user = existing
	} else {
		t.State["user"] = user
		if err := t.Save(t.State); err != nil {
			return nil, err
		}
	}
	password := sbcrypto.RandomString(32)
	if err := c.setUser(ctx, rc, user, prefix, password, true, false); err != nil {
		return nil, err
	}
	return &Provisioned{ExternalName: prefix, Credentials: Credentials{Username: user, Secret: password}}, nil
}

func (c *Cache) Deprovision(ctx context.Context, t *Target) error {
	rc := admin(t)
	defer rc.Close()
	users, err := rc.Do(ctx, "acl", "users").StringSlice()
	if err != nil {
		return err
	}
	base := cacheUser(t.R.ID)
	for _, u := range users {
		if strings.HasPrefix(u, base) {
			rc.Do(ctx, "client", "kill", "user", u)
			if err := rc.Do(ctx, "acl", "deluser", u).Err(); err != nil {
				return err
			}
		}
	}
	rc.Do(ctx, "acl", "save")
	// Delete the namespace in batches.
	prefix := KeyPrefix(t.R.ID)
	var cursor uint64
	for {
		keys, next, err := rc.Scan(ctx, cursor, prefix+"*", 500).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := rc.Unlink(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func (c *Cache) toggle(ctx context.Context, t *Target, creds []Credentials, enabled bool) error {
	rc := admin(t)
	defer rc.Close()
	for _, cr := range creds {
		if err := c.setUser(ctx, rc, cr.Username, KeyPrefix(t.R.ID), cr.Secret, enabled, t.R.WriteLocked); err != nil {
			return err
		}
		if !enabled {
			rc.Do(ctx, "client", "kill", "user", cr.Username)
		}
	}
	return nil
}

func (c *Cache) Pause(ctx context.Context, t *Target, creds []Credentials) error {
	return c.toggle(ctx, t, creds, false)
}

func (c *Cache) Resume(ctx context.Context, t *Target, creds []Credentials) error {
	return c.toggle(ctx, t, creds, true)
}

func (c *Cache) NewCredentials(ctx context.Context, t *Target) (*Credentials, error) {
	rc := admin(t)
	defer rc.Close()
	user := cacheUser(t.R.ID) + "_" + sbcrypto.RandomLower(4)
	password := sbcrypto.RandomString(32)
	if err := c.setUser(ctx, rc, user, KeyPrefix(t.R.ID), password, true, t.R.WriteLocked); err != nil {
		return nil, err
	}
	return &Credentials{Username: user, Secret: password}, nil
}

func (c *Cache) RevokeCredentials(ctx context.Context, t *Target, cr Credentials) error {
	rc := admin(t)
	defer rc.Close()
	rc.Do(ctx, "client", "kill", "user", cr.Username)
	if err := rc.Do(ctx, "acl", "deluser", cr.Username).Err(); err != nil {
		return err
	}
	rc.Do(ctx, "acl", "save")
	return nil
}

func (c *Cache) ApplyLimits(ctx context.Context, t *Target, creds []Credentials) error {
	// Memory and client limits are enforced by sampling (MeasureUsage and
	// EnforceClients); there is nothing to set on the ACL user.
	return nil
}

// MeasureUsage estimates memory by sampling keys under the prefix.
func (c *Cache) MeasureUsage(ctx context.Context, t *Target) (Usage, error) {
	rc := admin(t)
	defer rc.Close()
	prefix := KeyPrefix(t.R.ID)
	var cursor uint64
	var keys, sampled, sampledBytes int64
	for {
		batch, next, err := rc.Scan(ctx, cursor, prefix+"*", 1000).Result()
		if err != nil {
			return nil, err
		}
		keys += int64(len(batch))
		for i, k := range batch {
			// Measure every key up to 5000, then one in ten.
			if sampled < 5000 || i%10 == 0 {
				n, err := rc.MemoryUsage(ctx, k).Result()
				if err == nil {
					sampled++
					sampledBytes += n
				}
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	var mem int64
	if sampled > 0 {
		mem = sampledBytes * keys / sampled
	}
	clients, err := c.clientCount(ctx, rc, cacheUser(t.R.ID))
	if err != nil {
		return nil, err
	}
	return Usage{"memory_bytes": mem, "keys": keys, "connections": int64(clients)}, nil
}

func (c *Cache) clientCount(ctx context.Context, rc *redis.Client, userBase string) (int, error) {
	list, err := rc.ClientList(ctx).Result()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(list, "\n") {
		if strings.Contains(line, " user="+userBase) {
			n++
		}
	}
	return n, nil
}

// EnforceClients kills the newest clients above the limit.
func (c *Cache) EnforceClients(ctx context.Context, t *Target, limit int) (killed int, err error) {
	rc := admin(t)
	defer rc.Close()
	list, err := rc.ClientList(ctx).Result()
	if err != nil {
		return 0, err
	}
	var ids []string
	for _, line := range strings.Split(list, "\n") {
		if !strings.Contains(line, " user="+cacheUser(t.R.ID)) {
			continue
		}
		for _, f := range strings.Fields(line) {
			if strings.HasPrefix(f, "id=") {
				ids = append(ids, strings.TrimPrefix(f, "id="))
			}
		}
	}
	// Client ids grow over time; the list is in creation order.
	for i := limit; i < len(ids); i++ {
		if err := rc.Do(ctx, "client", "kill", "id", ids[i]).Err(); err == nil {
			killed++
		}
	}
	return killed, nil
}

func (c *Cache) SetWriteLock(ctx context.Context, t *Target, creds []Credentials, locked bool) error {
	rc := admin(t)
	defer rc.Close()
	enabled := t.R.Status == "active" || t.R.Status == "provisioning"
	for _, cr := range creds {
		if err := c.setUser(ctx, rc, cr.Username, KeyPrefix(t.R.ID), cr.Secret, enabled, locked); err != nil {
			return fmt.Errorf("set acl: %w", err)
		}
	}
	return nil
}
