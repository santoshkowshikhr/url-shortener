package model

import (
	"context"
	"log"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client
var ctx = context.Background()

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
