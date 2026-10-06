package core

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
)

// RateLimiter is a sliding-window limiter on Redis sorted sets.
type RateLimiter struct {
	R      *redis.Client
	Prefix string
}

type RateResult struct {
	Allowed   bool
	Limit     int
	Remaining int
	Reset     time.Time
}

var slidingWindow = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local count = redis.call('ZCARD', key)
local allowed = 0
if count < limit then
  redis.call('ZADD', key, now, member)
  count = count + 1
  allowed = 1
end
redis.call('PEXPIRE', key, window)
local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
local reset = now + window
if oldest[2] then reset = tonumber(oldest[2]) + window end
return {allowed, count, reset}
`)

// Allow records one hit for key and reports whether it fits in limit per window.
func (l *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (RateResult, error) {
	now := time.Now().UnixMilli()
	res, err := slidingWindow.Run(ctx, l.R, []string{l.Prefix + "rl:" + key},
		now, window.Milliseconds(), limit, strconv.FormatInt(now, 10)+"-"+sbcrypto.RandomLower(6)).Int64Slice()
	if err != nil {
		return RateResult{}, err
	}
	remaining := limit - int(res[1])
	if remaining < 0 {
		remaining = 0
	}
	return RateResult{Allowed: res[0] == 1, Limit: limit, Remaining: remaining, Reset: time.UnixMilli(res[2])}, nil
}

// Counter tracks monthly project API requests per service.
type Counter struct {
	R      *redis.Client
	Prefix string
}

func monthKey(t time.Time) string { return t.UTC().Format("2006-01") }

// Incr adds one request and returns the month's total.
func (c *Counter) Incr(ctx context.Context, resourceID string) (int64, error) {
	key := c.Prefix + "req:" + resourceID + ":" + monthKey(time.Now())
	pipe := c.R.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 40*24*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

// Get returns the month's total without counting.
func (c *Counter) Get(ctx context.Context, resourceID string) (int64, error) {
	v, err := c.R.Get(ctx, c.Prefix+"req:"+resourceID+":"+monthKey(time.Now())).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return v, err
}
