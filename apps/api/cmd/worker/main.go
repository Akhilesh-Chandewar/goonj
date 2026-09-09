// Command worker runs Goonj background jobs (audio processing, analytics,
// notifications) on an Asynq queue backed by Redis. Phase 0 starts the
// server with the audio-pipeline task registered as a placeholder.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/platform"
)

// Task types handled by this worker.
const (
	TypeProcessAudio = "audio:process"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg := platform.Load("goonj-worker")
	logger := platform.NewLogger(cfg)
	slog.SetDefault(logger)

	redisOpt, err := asynq.ParseRedisURI(cfg.RedisURL)
	if err != nil {
		return err
	}

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 4,
		RetryDelayFunc: asynq.DefaultRetryDelayFunc,
	})

	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeProcessAudio, handleProcessAudio(logger))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("worker starting", slog.String("queue", cfg.RedisURL))
		errCh <- srv.Run(mux) // blocks until shutdown
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		srv.Shutdown()
		return nil
	}
}

// handleProcessAudio is a Phase 0 placeholder; FFmpeg transcoding, waveform
// generation and loudness normalization arrive with the Phase 1 pipeline.
func handleProcessAudio(logger *slog.Logger) func(context.Context, *asynq.Task) error {
	return func(_ context.Context, t *asynq.Task) error {
		logger.Info("audio processing task received (pipeline lands in Phase 1)",
			slog.String("type", t.Type()))
		return nil
	}
}
