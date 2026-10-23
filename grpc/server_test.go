package grpcserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"go-api-server/api"
	grpcserver "go-api-server/grpc"
	"go-api-server/model"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type cacheHook redis.ProcessHook

func (h cacheHook) DialHook(next redis.DialHook) redis.DialHook     { return next }
func (h cacheHook) ProcessHook(redis.ProcessHook) redis.ProcessHook { return redis.ProcessHook(h) }
func (h cacheHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func setupDB(t *testing.T) {
	t.Helper()
	model.InitDB(filepath.Join(t.TempDir(), "shortener.db"))
	previous := model.RDB
	model.RDB = nil
	t.Cleanup(func() { model.RDB = previous })
}

func setupCache(t *testing.T, process redis.ProcessHook) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "cache.invalid:6379"})
	client.AddHook(cacheHook(process))
	model.RDB = client
	t.Cleanup(func() { require.NoError(t, client.Close()) })
}

func TestCreateShortURL_Basic(t *testing.T) {
	setupDB(t)
	server := &grpcserver.Server{}
	resp, err := server.CreateShortener(context.Background(), &api.ShortenerRequest{
		LongUrl:      "https://www.educative.io/courses/grokking-the-system-design-interview",
		CustomAlias:  "testalias",
		ExpireInDays: 3,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Contains(t, resp.ShortUrl, "testalias")
}

func TestRedirectShortener_WithoutRedis(t *testing.T) {
	setupDB(t)
	require.NoError(t, model.InsertURLMapping("https://example.com/without-cache", "nocache", "nocache", nil))

	resp, err := (&grpcserver.Server{}).RedirectShortener(context.Background(), &api.RedirectRequest{ShortUrl: "nocache"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "https://example.com/without-cache", resp.LongUrl)
}

func TestRedirectShortener_ExpiredWarmCache(t *testing.T) {
	setupDB(t)
	expireAt := time.Now().Add(-time.Hour)
	const longURL = "https://example.com/expired"
	require.NoError(t, model.InsertURLMapping(longURL, "expiredwarm", "expiredwarm", &expireAt))
	record, err := json.Marshal(map[string]any{"long_url": longURL, "expire_at": expireAt})
	require.NoError(t, err)
	setupCache(t, func(ctx context.Context, cmd redis.Cmder) error {
		require.Equal(t, "get", cmd.Name())
		value := string(record)
		if cmd.Args()[1] == "short:expiredwarm" {
			value = longURL
		}
		cmd.(*redis.StringCmd).SetVal(value)
		return nil
	})

	resp, err := (&grpcserver.Server{}).RedirectShortener(context.Background(), &api.RedirectRequest{ShortUrl: "expiredwarm"})

	require.Equal(t, codes.NotFound, status.Code(err))
	require.Nil(t, resp)
}

func TestRedirectShortener_ColdCache(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remaining time.Duration
		permanent bool
		want      codes.Code
	}{
		{name: "valid", remaining: time.Hour},
		{name: "permanent", permanent: true},
		{name: "expired", remaining: -time.Hour, want: codes.NotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupDB(t)
			var expireAt *time.Time
			if !tc.permanent {
				expiry := time.Now().Add(tc.remaining)
				expireAt = &expiry
			}
			const longURL = "https://example.com/redirect"
			require.NoError(t, model.InsertURLMapping(longURL, "cold", "cold", expireAt))
			var cached string
			writes := 0
			setupCache(t, func(ctx context.Context, cmd redis.Cmder) error {
				require.Equal(t, "short:v2:cold", cmd.Args()[1])
				if cmd.Name() == "get" {
					if cached == "" {
						return redis.Nil
					}
					cmd.(*redis.StringCmd).SetVal(cached)
					return nil
				}
				require.Equal(t, "set", cmd.Name())
				cached = string(cmd.Args()[2].([]byte))
				writes++
				return nil
			})

			server := &grpcserver.Server{}
			for range 2 {
				resp, err := server.RedirectShortener(context.Background(), &api.RedirectRequest{ShortUrl: "cold"})
				require.Equal(t, tc.want, status.Code(err))
				if tc.want != codes.OK {
					require.Nil(t, resp)
				} else {
					require.NotNil(t, resp)
					require.Equal(t, longURL, resp.LongUrl)
				}
			}
			if tc.want == codes.OK {
				require.Equal(t, 1, writes)
			} else {
				require.Zero(t, writes)
			}
		})
	}
}

func TestRedirectShortener_WarmCache(t *testing.T) {
	for _, record := range []string{
		`{"long_url":"https://example.com/hot"}`,
		`{"long_url":"https://example.com/hot","expire_at":"2999-01-01T00:00:00Z"}`,
	} {
		t.Run(record, func(t *testing.T) {
			setupDB(t)
			setupCache(t, func(ctx context.Context, cmd redis.Cmder) error {
				require.Equal(t, []any{"get", "short:v2:hot"}, cmd.Args())
				cmd.(*redis.StringCmd).SetVal(record)
				return nil
			})

			resp, err := (&grpcserver.Server{}).RedirectShortener(context.Background(), &api.RedirectRequest{ShortUrl: "hot"})

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, "https://example.com/hot", resp.LongUrl)
		})
	}
}

func TestRedirectShortener_CacheFallback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  string
		getErr error
	}{
		{name: "transport error", getErr: errors.New("cache unavailable")},
		{name: "malformed record", value: `not JSON`},
		{name: "empty URL", value: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupDB(t)
			require.NoError(t, model.InsertURLMapping("https://example.com/fallback", "fallback", "fallback", nil))
			writes := 0
			setupCache(t, func(ctx context.Context, cmd redis.Cmder) error {
				if cmd.Name() == "get" {
					cmd.(*redis.StringCmd).SetVal(tc.value)
					return tc.getErr
				}
				writes++
				return errors.New("cache write unavailable")
			})

			resp, err := (&grpcserver.Server{}).RedirectShortener(context.Background(), &api.RedirectRequest{ShortUrl: "fallback"})

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, "https://example.com/fallback", resp.LongUrl)
			require.Equal(t, 1, writes)
		})
	}
}

func TestRedirectShortener_NotFound(t *testing.T) {
	setupDB(t)
	setupCache(t, func(ctx context.Context, cmd redis.Cmder) error {
		require.Equal(t, "get", cmd.Name())
		return redis.Nil
	})

	resp, err := (&grpcserver.Server{}).RedirectShortener(context.Background(), &api.RedirectRequest{ShortUrl: "missing"})

	require.Equal(t, codes.NotFound, status.Code(err))
	require.Nil(t, resp)
}
