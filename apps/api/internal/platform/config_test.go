package platform

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("GOONJ_ENV", "")
	t.Setenv("HTTP_PORT", "")
	cfg := Load("test-service")

	if cfg.Env != "development" {
		t.Errorf("Env = %q, want development", cfg.Env)
	}
	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort = %q, want 8080", cfg.HTTPPort)
	}
	if cfg.ServiceName != "test-service" {
		t.Errorf("ServiceName = %q, want test-service", cfg.ServiceName)
	}
	if cfg.ShutdownGracePeriod != 15*time.Second {
		t.Errorf("ShutdownGracePeriod = %v, want 15s", cfg.ShutdownGracePeriod)
	}
	if cfg.IsProduction() {
		t.Error("IsProduction() = true, want false for development")
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("GOONJ_ENV", "production")
	t.Setenv("HTTP_PORT", "9090")
	t.Setenv("ALLOWED_ORIGINS", "https://goonj.app, https://www.goonj.app")
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "30s")

	cfg := Load("test-service")

	if !cfg.IsProduction() {
		t.Error("IsProduction() = false, want true")
	}
	if cfg.HTTPPort != "9090" {
		t.Errorf("HTTPPort = %q, want 9090", cfg.HTTPPort)
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[0] != "https://goonj.app" {
		t.Errorf("AllowedOrigins = %v, want two origins", cfg.AllowedOrigins)
	}
	if cfg.ShutdownGracePeriod != 30*time.Second {
		t.Errorf("ShutdownGracePeriod = %v, want 30s", cfg.ShutdownGracePeriod)
	}
}

func TestLoadInvalidDurationFallsBack(t *testing.T) {
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "not-a-duration")
	cfg := Load("test-service")
	if cfg.ShutdownGracePeriod != 15*time.Second {
		t.Errorf("ShutdownGracePeriod = %v, want default 15s", cfg.ShutdownGracePeriod)
	}
}
