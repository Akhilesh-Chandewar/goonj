package platform

import (
	"log/slog"
	"os"
)

// NewLogger builds the process-wide structured logger: JSON in production,
// human-friendly text locally. Every log line carries the service name and
// environment so logs from mixed deployments stay greppable.
func NewLogger(cfg Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.IsProduction() {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler).
		With(slog.String("service", cfg.ServiceName), slog.String("env", cfg.Env))
}
