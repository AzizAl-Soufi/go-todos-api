// Package dbtest gives integration tests a real, migrated, throwaway
// database. Tests skip unless the matching environment variable is set:
//
//	TEST_POSTGRES_URI  e.g. postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable
//	TEST_MONGO_URI     e.g. mongodb://localhost:27017/?replicaSet=rs0
//
// Every call creates its own database and drops it when the test ends, so
// tests can run in parallel and never see each other's data. `make
// test-integration` starts both servers in Docker and sets the variables.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	mongomigrations "github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
	_ "github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations/versions"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	PostgresEnv = "TEST_POSTGRES_URI"
	MongoEnv    = "TEST_MONGO_URI"

	setupTimeout = time.Minute
)

// Postgres returns a pool on a new database with every migration applied.
func Postgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool := PostgresEmpty(t)
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return pool
}

// PostgresEmpty returns a pool on a new database without any migration
// applied, for testing the migrations themselves.
func PostgresEmpty(t testing.TB) *pgxpool.Pool {
	t.Helper()
	uri := os.Getenv(PostgresEnv)
	if uri == "" {
		t.Skipf("%s not set; skipping PostgreSQL integration test", PostgresEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	admin, err := pgxpool.New(ctx, uri)
	if err != nil {
		t.Fatalf("connect to %s: %v", PostgresEnv, err)
	}
	name := uniqueName()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close()
		t.Fatalf("create test database: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(uri)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop test database %s: %v", name, err)
		}
		admin.Close()
	})

	return pool
}

// Mongo returns a client on a new database with every migration applied (no
// seed data).
func Mongo(t testing.TB) mongodb.MongoDBClient {
	t.Helper()
	uri := os.Getenv(MongoEnv)
	if uri == "" {
		t.Skipf("%s not set; skipping MongoDB integration test", MongoEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	client, err := mongodb.New(ctx, &config.DatabaseConfig{DatabaseURI: uri, DatabaseName: uniqueName()})
	if err != nil {
		t.Fatalf("connect to %s: %v", MongoEnv, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
		defer cancel()
		if err := client.Database().Drop(ctx); err != nil {
			t.Errorf("drop test database: %v", err)
		}
		_ = client.Close(ctx)
	})
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("ping %s: %v", MongoEnv, err)
	}

	var migCfg mongomigrations.MigrationConfig
	migCfg.MongoDB.SchemaDir = mongoSchemaDir()
	logger := log.New(io.Discard, "", 0)
	if err := mongomigrations.NewMigration(client.Database(), logger, &migCfg).RunMigrations(); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return client
}

// mongoSchemaDir finds the collection schemas (which define the indexes)
// relative to this file, so it works from any package's test directory.
func mongoSchemaDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "mongodb", "migrations", "schema")
}

func uniqueName() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "test_" + hex.EncodeToString(b)
}
