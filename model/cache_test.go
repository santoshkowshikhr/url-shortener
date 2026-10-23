package model

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type cacheHook redis.ProcessHook

func (h cacheHook) DialHook(next redis.DialHook) redis.DialHook     { return next }
func (h cacheHook) ProcessHook(redis.ProcessHook) redis.ProcessHook { return redis.ProcessHook(h) }
func (h cacheHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func setupCache(t *testing.T, process redis.ProcessHook) {
	t.Helper()
	previous := RDB
	RDB = redis.NewClient(&redis.Options{Addr: "cache.invalid:6379"})
	RDB.AddHook(cacheHook(process))
	t.Cleanup(func() {
		require.NoError(t, RDB.Close())
		RDB = previous
	})
}

func TestCacheRecordValidAt(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		expireAt *time.Time
		valid    bool
	}{
		{name: "permanent", valid: true},
		{name: "before deadline", expireAt: timePtr(now.Add(time.Nanosecond)), valid: true},
		{name: "exact deadline", expireAt: &now},
		{name: "after deadline", expireAt: timePtr(now.Add(-time.Nanosecond))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.valid, (cacheRecord{LongURL: "https://example.com", ExpireAt: tc.expireAt}).validAt(now))
		})
	}
	require.False(t, (cacheRecord{}).validAt(now))
}

func TestCacheTTL(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		expireAt *time.Time
		want     time.Duration
	}{
		{name: "permanent", want: 5 * time.Minute},
		{name: "long lived", expireAt: timePtr(now.Add(time.Hour)), want: 5 * time.Minute},
		{name: "short lived", expireAt: timePtr(now.Add(90 * time.Second)), want: 90 * time.Second},
		{name: "submillisecond", expireAt: timePtr(now.Add(500 * time.Microsecond))},
		{name: "fractional millisecond", expireAt: timePtr(now.Add(1500 * time.Microsecond)), want: time.Millisecond},
		{name: "exact deadline", expireAt: &now},
		{name: "expired", expireAt: timePtr(now.Add(-time.Millisecond)), want: -time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, cacheTTL(tc.expireAt, now)) })
	}
}

func TestGetCachedURL(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  string
		getErr error
		want   string
		miss   bool
	}{
		{name: "permanent", value: `{"long_url":"https://example.com"}`, want: "https://example.com"},
		{name: "valid", value: `{"long_url":"https://example.com","expire_at":"2999-01-01T00:00:00Z"}`, want: "https://example.com"},
		{name: "expired", value: `{"long_url":"https://example.com","expire_at":"2000-01-01T00:00:00Z"}`, miss: true},
		{name: "empty URL", value: `{}`, miss: true},
		{name: "missing", getErr: redis.Nil, miss: true},
		{name: "malformed", value: `https://example.com`},
		{name: "invalid expiry", value: `{"long_url":"https://example.com","expire_at":"invalid"}`},
		{name: "transport error", getErr: errors.New("cache unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupCache(t, func(ctx context.Context, cmd redis.Cmder) error {
				require.Equal(t, []any{"get", "short:v2:code"}, cmd.Args())
				cmd.(*redis.StringCmd).SetVal(tc.value)
				return tc.getErr
			})

			got, err := GetCachedURL(context.Background(), "code")

			require.Equal(t, tc.want, got)
			if tc.want != "" {
				require.NoError(t, err)
			} else if tc.miss {
				require.ErrorIs(t, err, redis.Nil)
			} else {
				require.Error(t, err)
				if tc.getErr != nil {
					require.ErrorIs(t, err, tc.getErr)
				}
			}
		})
	}
}

func TestCacheWithoutRedis(t *testing.T) {
	previous := RDB
	RDB = nil
	t.Cleanup(func() { RDB = previous })

	got, err := GetCachedURL(context.Background(), "code")

	require.Empty(t, got)
	require.ErrorIs(t, err, redis.Nil)
	require.NoError(t, CacheURL(context.Background(), "code", "https://example.com", nil))
}

func TestCacheURLCommands(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remaining time.Duration
		permanent bool
		write     bool
	}{
		{name: "permanent", permanent: true, write: true},
		{name: "long lived", remaining: time.Hour, write: true},
		{name: "short lived", remaining: 90 * time.Second, write: true},
		{name: "submillisecond", remaining: 500 * time.Microsecond},
		{name: "expired", remaining: -time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commands [][]any
			setupCache(t, func(ctx context.Context, cmd redis.Cmder) error {
				commands = append(commands, cmd.Args())
				return nil
			})
			var expireAt *time.Time
			if !tc.permanent {
				expireAt = timePtr(time.Now().Add(tc.remaining))
			}

			started := time.Now()
			require.NoError(t, CacheURL(context.Background(), "code", "https://example.com", expireAt))

			if !tc.write {
				require.Empty(t, commands)
				return
			}
			require.Len(t, commands, 1)
			args := commands[0]
			require.Len(t, args, 5)
			require.Equal(t, []any{"set", "short:v2:code"}, args[:2])
			var record cacheRecord
			require.NoError(t, json.Unmarshal(args[2].([]byte), &record))
			require.Equal(t, "https://example.com", record.LongURL)
			if expireAt == nil {
				require.Nil(t, record.ExpireAt)
			} else {
				require.NotNil(t, record.ExpireAt)
				require.True(t, expireAt.Equal(*record.ExpireAt))
			}
			if tc.permanent || tc.remaining > 5*time.Minute {
				require.Equal(t, []any{"ex", int64(300)}, args[3:])
			} else {
				require.Equal(t, "px", args[3])
				ttl := time.Duration(args[4].(int64)) * time.Millisecond
				require.Positive(t, ttl)
				require.LessOrEqual(t, ttl, expireAt.Sub(started))
			}
		})
	}
}

func TestCacheURLWriteError(t *testing.T) {
	want := errors.New("cache write unavailable")
	setupCache(t, func(ctx context.Context, cmd redis.Cmder) error { return want })

	require.ErrorIs(t, CacheURL(context.Background(), "code", "https://example.com", nil), want)
}

func timePtr(t time.Time) *time.Time { return &t }
