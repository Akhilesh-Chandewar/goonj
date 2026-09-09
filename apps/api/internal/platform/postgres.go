package platform

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgres opens a connection pool and verifies connectivity with a ping.
func NewPostgres(ctx context.Context, cfg Config, logger *slog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	logger.Info("postgres connected")
	return pool, nil
}

// PostgresHealth adapts a pool to the Checker interface for /readyz.
type PostgresHealth struct {
	Pool *pgxpool.Pool
}

// CheckHealth implements platform.Checker.
func (h PostgresHealth) CheckHealth(ctx context.Context) HealthStatus {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := h.Pool.Ping(ctx); err != nil {
		return HealthStatus{Status: "unavailable", Error: err.Error()}
	}
	return HealthStatus{Status: "ok"}
}
