package redis

import (
	"context"
	"fmt"

	"github.com/go-redis/redis/v8"

	"github.com/zanehu-ai/x-go/config"
)

// New initializes a Redis client and verifies connectivity with a ping.
func New(cfg config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return client, nil
}
