// Command api runs the Goonj REST API service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/app"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/health"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/platform"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg := platform.Load("goonj-api")
	logger := platform.NewLogger(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Postgres and Redis are wired in Phase 1; the API starts healthy without
	// them so Phase 0 can boot against an empty compose stack.
	pool, err := platform.NewPostgres(ctx, cfg, logger)
	if err != nil {
		logger.Warn("postgres unavailable, continuing degraded", slog.Any("error", err))
	}
	rdb, err := platform.NewRedis(ctx, cfg, logger)
	if err != nil {
		logger.Warn("redis unavailable, continuing degraded", slog.Any("error", err))
	}

	var checks []platform.Checker
	healthServices := []health.Service{
		{Name: "api", Status: func(_ context.Context) string { return "ok" }},
	}
	if pool != nil {
		checks = append(checks, platform.PostgresHealth{Pool: pool})
		healthServices = append(healthServices, health.Service{
			Name:   "postgres",
			Status: func(ctx context.Context) string { return pingPostgres(pool, ctx) },
		})
	}
	if rdb != nil {
		checks = append(checks, platform.RedisHealth{Client: rdb})
		healthServices = append(healthServices, health.Service{
			Name:   "redis",
			Status: func(ctx context.Context) string { return pingRedis(rdb, ctx) },
		})
	}

	healthMod := health.NewModule(healthServices...)
	authMod := auth.NewModule()
	application := app.New(logger, healthMod, authMod, cfg.AllowedOrigins)

	server := platform.NewHTTPServer(cfg, logger, application.Handler(), checks...)

	errCh := make(chan error, 1)
	go func() { errCh <- server.Start() }()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGracePeriod)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func pingPostgres(pool *pgxpool.Pool, ctx context.Context) string {
	if err := pool.Ping(ctx); err != nil {
		return "unavailable"
	}
	return "ok"
}

func pingRedis(rdb *redis.Client, ctx context.Context) string {
	if err := rdb.Ping(ctx).Err(); err != nil {
		return "unavailable"
	}
	return "ok"
}
