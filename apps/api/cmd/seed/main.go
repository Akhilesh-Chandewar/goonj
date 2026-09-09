// Command seed runs migrations and inserts development data. Safe to re-run:
// migrations are versioned by goose and seed rows use ON CONFLICT.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/platform"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg := platform.Load("goonj-seed")
	logger := platform.NewLogger(cfg)
	slog.SetDefault(logger)

	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}
	if err := platform.Migrate(cfg.DatabaseURL, migrationsDir); err != nil {
		return err
	}
	logger.Info("migrations applied", slog.String("dir", migrationsDir))

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect for seed: %w", err)
	}
	defer conn.Close(ctx)

	// Idempotent demo data: one admin user and their channel.
	const seedSQL = `
	INSERT INTO users (email, password_hash, role)
	VALUES ('admin@goonj.local', '$2a$10$REPLACE_WITH_BCRYPT_HASH', 'ADMIN')
	ON CONFLICT (email) DO NOTHING
	RETURNING id`

	var userID string
	if err := conn.QueryRow(ctx, seedSQL).Scan(&userID); err != nil {
		if err == pgx.ErrNoRows {
			logger.Info("seed data already present; nothing to do")
			return nil
		}
		return fmt.Errorf("seed user: %w", err)
	}

	_, err = conn.Exec(ctx, `
		INSERT INTO profiles (user_id, username, display_name, bio)
		VALUES ($1, 'admin', 'Goonj Admin', 'Platform administrator')
		ON CONFLICT (user_id) DO NOTHING`, userID)
	if err != nil {
		return fmt.Errorf("seed profile: %w", err)
	}

	logger.Info("seed complete", slog.String("user_id", userID))
	return nil
}
