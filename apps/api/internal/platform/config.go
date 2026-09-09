// Package platform provides shared infrastructure primitives for all Goonj
// services: configuration, logging, database, cache, migrations and HTTP
// server wiring. Services must not duplicate these concerns locally.
package platform

import (
	"os"
	"strings"
	"time"
)

// Config holds runtime configuration for a service. Values load from
// environment variables with development defaults so the stack boots with
// zero configuration locally (see .env.example for the full list).
type Config struct {
	Env            string
	ServiceName    string
	HTTPPort       string
	DatabaseURL    string
	RedisURL       string
	LogLevel       string
	AllowedOrigins []string

	ShutdownGracePeriod time.Duration
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
}

// Load reads configuration from the environment for the named service.
func Load(service string) Config {
	return Config{
		Env:                 getEnv("GOONJ_ENV", "development"),
		ServiceName:         service,
		HTTPPort:            getEnv("HTTP_PORT", "8080"),
		DatabaseURL:         getEnv("DATABASE_URL", "postgres://goonj:goonj@localhost:5432/goonj?sslmode=disable"),
		RedisURL:            getEnv("REDIS_URL", "redis://localhost:6379/0"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		AllowedOrigins:      splitCSV(getEnv("ALLOWED_ORIGINS", "http://localhost:3000")),
		ShutdownGracePeriod: getDuration("SHUTDOWN_GRACE_PERIOD", 15*time.Second),
		ReadTimeout:         getDuration("HTTP_READ_TIMEOUT", 10*time.Second),
		WriteTimeout:        getDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
	}
}

// IsProduction reports whether the service runs in production mode.
func (c Config) IsProduction() bool { return c.Env == "production" }

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
