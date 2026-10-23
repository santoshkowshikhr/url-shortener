package model

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const cacheRetention = 5 * time.Minute

var RDB *redis.Client
var ctx = context.Background()

type cacheRecord struct {
	LongURL  string     `json:"long_url"`
	ExpireAt *time.Time `json:"expire_at,omitempty"`
}

func (r cacheRecord) validAt(now time.Time) bool {
	return r.LongURL != "" && (r.ExpireAt == nil || now.Before(*r.ExpireAt))
}

func GetCachedURL(ctx context.Context, shortCode string) (string, error) {
	if RDB == nil {
		return "", redis.Nil
	}
	value, err := RDB.Get(ctx, "short:v2:"+shortCode).Result()
	if err != nil {
		return "", err
	}
	var record cacheRecord
	if err := json.Unmarshal([]byte(value), &record); err != nil {
		return "", err
	}
	if !record.validAt(time.Now()) {
		return "", redis.Nil
	}
	return record.LongURL, nil
}

func CacheURL(ctx context.Context, shortCode, longURL string, expireAt *time.Time) error {
	if RDB == nil {
		return nil
	}
	ttl := cacheTTL(expireAt, time.Now())
	if ttl <= 0 {
		return nil
	}
	value, err := json.Marshal(cacheRecord{LongURL: longURL, ExpireAt: expireAt})
	if err != nil {
		return err
	}
	return RDB.Set(ctx, "short:v2:"+shortCode, value, ttl).Err()
}

func cacheTTL(expireAt *time.Time, now time.Time) time.Duration {
	if expireAt == nil {
		return cacheRetention
	}
	// Round down so Redis's millisecond precision cannot extend the deadline.
	return min(cacheRetention, expireAt.Sub(now)).Truncate(time.Millisecond)
}

func InitRedis() {
	RDB = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // or from env
		DB:       0,
	})

	if _, err := RDB.Ping(ctx).Result(); err != nil {
		log.Fatalf("Redis ping failed: %v", err)
	}

	log.Println("Redis ping successful")
}
