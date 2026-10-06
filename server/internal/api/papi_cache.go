package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shellhaki/sparkbase/server/internal/drivers"
	"github.com/shellhaki/sparkbase/server/internal/httpx"
	"github.com/shellhaki/sparkbase/server/internal/store"
)

const maxCacheValue = 1 << 20

func cacheKey(res *store.Resource, key string) (string, error) {
	if key == "" || len(key) > 512 {
		return "", httpx.Invalid("key must be 1 to 512 bytes")
	}
	return drivers.KeyPrefix(res.ID) + key, nil
}

// cacheError maps Redis errors, including ACL denials from a write lock.
func cacheError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "NOPERM") {
		return httpx.Quota("the cache is over its memory limit; writes are blocked until keys are deleted")
	}
	if strings.Contains(msg, "WRONGTYPE") {
		return httpx.E(400, "wrong_type", "the key holds a value that is not a string")
	}
	if strings.Contains(msg, "WRONGPASS") || strings.Contains(msg, "NOAUTH") || strings.Contains(msg, "user is disabled") {
		return httpx.NotReady("unavailable")
	}
	return err
}

var globEscaper = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`)

func (s *Server) cacheClient(r *http.Request, pc *pctx) (*store.Resource, *redis.Client, error) {
	res, err := s.service(r, pc, "cache", r.PathValue("name"))
	if err != nil {
		return nil, nil, err
	}
	c, err := s.Tenants.Redis(r.Context(), res)
	return res, c, err
}

func (s *Server) cacheRoutes() {
	s.papi("GET /p/{ref}/cache/{name}/keys/{key...}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, c, err := s.cacheClient(r, pc)
		if err != nil {
			return err
		}
		k, err := cacheKey(res, r.PathValue("key"))
		if err != nil {
			return err
		}
		ctx := r.Context()
		pipe := c.Pipeline()
		get := pipe.Get(ctx, k)
		ttl := pipe.PTTL(ctx, k)
		_, err = pipe.Exec(ctx)
		if errors.Is(err, redis.Nil) || errors.Is(get.Err(), redis.Nil) {
			return httpx.NotFound("key")
		}
		if err != nil {
			return cacheError(err)
		}
		var ttlSecs any
		if d := ttl.Val(); d > 0 {
			ttlSecs = int64(d / time.Second)
		}
		return httpx.OK(w, map[string]any{"key": r.PathValue("key"), "value": get.Val(), "ttl": ttlSecs})
	})

	s.papi("PUT /p/{ref}/cache/{name}/keys/{key...}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Value *string `json:"value"`
			TTL   int64   `json:"ttl"`
			NX    bool    `json:"nx"`
		}
		if err := httpx.Decode(r, &in, maxCacheValue+1024); err != nil {
			return err
		}
		if in.Value == nil {
			return httpx.Invalid("value is required (a string; encode other types as JSON)")
		}
		if in.TTL < 0 {
			return httpx.Invalid("ttl must be positive seconds")
		}
		res, c, err := s.cacheClient(r, pc)
		if err != nil {
			return err
		}
		k, err := cacheKey(res, r.PathValue("key"))
		if err != nil {
			return err
		}
		ttl := time.Duration(in.TTL) * time.Second
		if in.NX {
			ok, err := c.SetNX(r.Context(), k, *in.Value, ttl).Result()
			if err != nil {
				return cacheError(err)
			}
			return httpx.OK(w, map[string]any{"set": ok})
		}
		if err := c.Set(r.Context(), k, *in.Value, ttl).Err(); err != nil {
			return cacheError(err)
		}
		return httpx.OK(w, map[string]any{"set": true})
	})

	s.papi("DELETE /p/{ref}/cache/{name}/keys/{key...}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, c, err := s.cacheClient(r, pc)
		if err != nil {
			return err
		}
		k, err := cacheKey(res, r.PathValue("key"))
		if err != nil {
			return err
		}
		n, err := c.Del(r.Context(), k).Result()
		if err != nil {
			return cacheError(err)
		}
		return httpx.OK(w, map[string]any{"deleted": n > 0})
	})

	s.papi("POST /p/{ref}/cache/{name}/incr/{key...}", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			By  int64 `json:"by"`
			TTL int64 `json:"ttl"`
		}
		if err := httpx.Decode(r, &in, 0); err != nil {
			return err
		}
		if in.By == 0 {
			in.By = 1
		}
		res, c, err := s.cacheClient(r, pc)
		if err != nil {
			return err
		}
		k, err := cacheKey(res, r.PathValue("key"))
		if err != nil {
			return err
		}
		ctx := r.Context()
		pipe := c.TxPipeline()
		incr := pipe.IncrBy(ctx, k, in.By)
		if in.TTL > 0 {
			pipe.ExpireNX(ctx, k, time.Duration(in.TTL)*time.Second)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return cacheError(err)
		}
		return httpx.OK(w, map[string]any{"value": incr.Val()})
	})

	s.papi("POST /p/{ref}/cache/{name}/batch", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		var in struct {
			Ops []struct {
				Op    string  `json:"op"`
				Key   string  `json:"key"`
				Value *string `json:"value"`
				TTL   int64   `json:"ttl"`
			} `json:"ops"`
		}
		if err := httpx.Decode(r, &in, 4<<20); err != nil {
			return err
		}
		if len(in.Ops) == 0 || len(in.Ops) > 100 {
			return httpx.Invalid("send 1 to 100 operations")
		}
		res, c, err := s.cacheClient(r, pc)
		if err != nil {
			return err
		}
		ctx := r.Context()
		pipe := c.Pipeline()
		cmds := make([]redis.Cmder, len(in.Ops))
		for i, op := range in.Ops {
			k, err := cacheKey(res, op.Key)
			if err != nil {
				return err
			}
			switch op.Op {
			case "get":
				cmds[i] = pipe.Get(ctx, k)
			case "set":
				if op.Value == nil {
					return httpx.Invalid("set needs a value").With("op", i)
				}
				cmds[i] = pipe.Set(ctx, k, *op.Value, time.Duration(op.TTL)*time.Second)
			case "del":
				cmds[i] = pipe.Del(ctx, k)
			default:
				return httpx.Invalid("op must be get, set or del").With("op", i)
			}
		}
		_, _ = pipe.Exec(ctx)
		out := make([]map[string]any, len(cmds))
		for i, cmd := range cmds {
			item := map[string]any{"op": in.Ops[i].Op, "key": in.Ops[i].Key}
			err := cmd.Err()
			switch v := cmd.(type) {
			case *redis.StringCmd:
				if errors.Is(err, redis.Nil) {
					item["value"] = nil
				} else if err == nil {
					item["value"] = v.Val()
				}
			case *redis.IntCmd:
				item["deleted"] = v.Val() > 0
			}
			if err != nil && !errors.Is(err, redis.Nil) {
				var e *httpx.Error
				if errors.As(cacheError(err), &e) {
					item["error"] = e.Code
				} else {
					item["error"] = "failed"
				}
			}
			out[i] = item
		}
		return httpx.OK(w, map[string]any{"results": out})
	})

	// Listing uses the host admin connection with a MATCH pattern inside the
	// service's own prefix, because tenants may not run SCAN.
	s.papi("GET /p/{ref}/cache/{name}/keys", accessSecret, func(w http.ResponseWriter, r *http.Request, pc *pctx) error {
		res, err := s.service(r, pc, "cache", r.PathValue("name"))
		if err != nil {
			return err
		}
		t, _, err := s.Target(r.Context(), res)
		if err != nil {
			return err
		}
		rc := drivers.AdminClient(t.H.InternalAddr, t.AdminSecret)
		defer rc.Close()
		prefix := drivers.KeyPrefix(res.ID)
		pattern := prefix + globEscaper.Replace(r.URL.Query().Get("prefix")) + "*"
		limit := queryInt(r, "limit", 100, 1, 1000)
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		keys := []string{}
		var cursor uint64
		for len(keys) < limit {
			batch, next, err := rc.Scan(ctx, cursor, pattern, 500).Result()
			if err != nil {
				return err
			}
			for _, k := range batch {
				if len(keys) < limit {
					keys = append(keys, strings.TrimPrefix(k, prefix))
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		return httpx.OK(w, map[string]any{"keys": keys, "truncated": cursor != 0})
	})
}
