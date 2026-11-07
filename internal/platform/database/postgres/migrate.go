package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// The SQL migrations are compiled into the binary, so neither the API nor the
// migrate command needs the migrations directory at runtime.
//
//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Migrations returns the embedded goose migration files.
func Migrations() fs.FS {
	sub, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		panic(err) // the directory is embedded at compile time
	}
	return sub
}

// Migrate applies all pending migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, Migrations())
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		slog.Info("applied migration", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	return nil
}
