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

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/app"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/infrastructure/storage"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/audio"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/health"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/live"
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

	pool, err := platform.NewPostgres(ctx, cfg, logger)
	if err != nil {
		return err // auth/live/audio require the DB; fail fast.
	}
	rdb, err := platform.NewRedis(ctx, cfg, logger)
	if err != nil {
		return err
	}

	// Object storage (floci locally). Non-fatal at boot: upload endpoints
	// return errors if storage is down, the rest of the API stays up.
	objstore, err := storage.New(ctx, storage.Config{
		InternalEndpoint: cfg.S3InternalEndpoint,
		PublicEndpoint:   cfg.S3PublicEndpoint,
		Region:           cfg.S3Region,
		Bucket:           cfg.S3Bucket,
		AccessKeyID:      cfg.S3AccessKeyID,
		SecretAccessKey:  cfg.S3SecretAccessKey,
		UsePathStyle:     cfg.S3UsePathStyle,
	})
	if err != nil {
		logger.Warn("object storage unavailable, uploads disabled", slog.Any("error", err))
		objstore = nil
	}

	// Asynq producer (shared with the worker consumers).
	redisOpt, err := asynq.ParseRedisURI(cfg.RedisURL)
	if err != nil {
		return err
	}
	queue := asynq.NewClient(redisOpt)
	defer queue.Close()

	// Modules
	authMod := auth.NewModule(pool, rdb, cfg.JWTSecret, logger)
	liveMod := live.NewModule(pool, rdb, live.Config{
		StreamingHost:   cfg.LiveKitHost,
		StreamingAPIKey: cfg.LiveKitAPIKey,
		StreamingSecret: cfg.LiveKitSecret,
	}, logger)

	var audioMod *audio.Module
	if objstore != nil {
		audioMod = audio.NewModule(pool, objstore, queue, logger)
	}

	healthMod := health.NewModule(
		health.Service{Name: "api", Status: func(_ context.Context) string { return "ok" }},
		health.Service{Name: "postgres", Status: func(ctx context.Context) string { return pingPostgres(pool, ctx) }},
		health.Service{Name: "redis", Status: func(ctx context.Context) string { return pingRedis(rdb, ctx) }},
	)

	application := app.New(app.Deps{
		Log:            logger,
		AllowedOrigins: cfg.AllowedOrigins,
		Auth:           authMod,
		Live:           liveMod,
		Audio:          audioMod,
		Health:         healthMod,
	})

	server := platform.NewHTTPServer(cfg, logger, application.Handler(),
		platform.PostgresHealth{Pool: pool},
		platform.RedisHealth{Client: rdb},
	)

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
