package platform

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver for goose
)

// Migrate runs pending SQL migrations from dir against the database at dbURL.
// Used by the seed command and by release jobs; the API server never runs
// migrations implicitly.
func Migrate(dbURL, dir string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("open database for migrations: %w", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	if err := goose.UpContext(context.Background(), db, dir); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
