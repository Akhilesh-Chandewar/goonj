package platform

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewRedis connects to Redis and verifies connectivity with a ping.
func NewRedis(ctx context.Context, cfg Config, logger *slog.Logger) (*redis.Client, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	logger.Info("redis connected")
	return client, nil
}

// RedisHealth adapts a client to the Checker interface for /readyz.
type RedisHealth struct {
	Client *redis.Client
}

// CheckHealth implements platform.Checker.
func (h RedisHealth) CheckHealth(ctx context.Context) HealthStatus {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := h.Client.Ping(ctx).Err(); err != nil {
		return HealthStatus{Status: "unavailable", Error: err.Error()}
	}
	return HealthStatus{Status: "ok"}
}
