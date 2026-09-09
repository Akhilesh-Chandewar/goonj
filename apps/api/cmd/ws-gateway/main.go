// Command ws-gateway runs the Goonj WebSocket gateway for live sessions:
// chat, presence and reactions fan-out via Redis pub/sub. Phase 0 exposes a
// single echo socket per connection; live rooms arrive in Phase 2.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/platform"
)

func main() {
	if err := run(); err != nil {
		slog.Error("ws-gateway exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg := platform.Load("goonj-ws")
	logger := platform.NewLogger(cfg)
	slog.SetDefault(logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"alive"}`))
	})
	mux.HandleFunc("/ws", echoSocket(logger))

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("ws-gateway listening", slog.String("addr", srv.Addr))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGracePeriod)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// echoSocket accepts a connection and echoes frames back until the client
// disconnects. Replaced by the Redis pub/sub room hub in Phase 2.
func echoSocket(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			logger.Warn("websocket accept failed", slog.Any("error", err))
			return
		}
		defer conn.CloseNow()

		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		for {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				return // client went away or timed out
			}
			writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = conn.Write(writeCtx, msgType, data)
			writeCancel()
			if err != nil {
				return
			}
		}
	}
}
