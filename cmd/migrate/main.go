// Command migrate applies database migrations for the configured DB_TYPE.
//
//	migrate                                apply migrations (postgres: embedded SQL; mongo: MONGO_MIGRATIONS_CONFIG)
//	migrate -c ./cmd/migrate/mongo.yaml    apply MongoDB migrations with an explicit config
//	migrate generate <name> -c <config>    scaffold a new MongoDB migration file
//	migrate generate queries -c <config>   generate MongoDB repositories from the schemas
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
	_ "github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations/versions"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
)

var logger = log.New(os.Stdout, "[MIGRATE] ", log.LstdFlags)

func main() {
	if err := run(os.Args[1:]); err != nil {
		logger.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "generate" {
		return generate(args[1:])
	}

	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	mongoConfig := fs.String("c", "", "MongoDB migration config (defaults to MONGO_MIGRATIONS_CONFIG)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ctx := context.Background()

	switch cfg.DB.DBType {
	case "postgres":
		client, err := postgres.New(ctx, cfg.DB.DatabaseURI)
		if err != nil {
			return err
		}
		defer client.Close()
		return postgres.Migrate(ctx, client.Pool())

	case "mongo":
		path := *mongoConfig
		if path == "" {
			path = cfg.DB.MongoMigrationsConfig
		}
		migCfg, err := migrations.LoadConfig(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		client, err := mongodb.New(ctx, &cfg.DB)
		if err != nil {
			return err
		}
		defer client.Close(ctx)
		return migrations.NewMigration(client.Database(), logger, migCfg).RunMigrations()

	default:
		return fmt.Errorf("nothing to migrate for DB_TYPE=%q (want postgres or mongo)", cfg.DB.DBType)
	}
}

func generate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: migrate generate <name|queries> -c <config>")
	}
	name := args[0]

	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	configPath := fs.String("c", "", "MongoDB migration config")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *configPath == "" {
		return fmt.Errorf("-c <config> is required")
	}
	migCfg, err := migrations.LoadConfig(*configPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", *configPath, err)
	}

	if name == "queries" {
		if err := migrations.GenerateQueries(migCfg); err != nil {
			return fmt.Errorf("code generation failed: %w", err)
		}
		logger.Println("generated code for MongoDB collections")
		return nil
	}
	if err := migrations.GenerateMigrationFile(migCfg, name); err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}
	logger.Printf("generated migration: %s", name)
	return nil
}
