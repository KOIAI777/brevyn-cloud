package platform

import (
	"context"
	"strings"

	"github.com/redis/go-redis/v9"
)

func ConnectRedis(ctx context.Context, redisURL string) (*redis.Client, error) {
	var opts *redis.Options
	var err error
	if strings.HasPrefix(redisURL, "redis://") || strings.HasPrefix(redisURL, "rediss://") {
		opts, err = redis.ParseURL(redisURL)
		if err != nil {
			return nil, err
		}
	} else {
		opts = &redis.Options{Addr: redisURL}
	}

	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}
