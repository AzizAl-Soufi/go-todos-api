package main

import (
	"context"
	"log"
	"os"

	"github.com/AzizAl-Soufi/go-todos-api/internal/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/database/mongodb/migrations"
	_ "github.com/AzizAl-Soufi/go-todos-api/internal/database/mongodb/migrations/versions"
)

func main() {
	logger := log.New(os.Stdout, "[MIGRATE] ", log.LstdFlags)

	var configPath string
	var isGenerate bool
	var migrationName string

	// 1. Safely parse command line arguments
	if len(os.Args) >= 5 && os.Args[1] == "generate" {
		isGenerate = true
		migrationName = os.Args[2]
		if os.Args[3] == "-c" || os.Args[3] == "--config" {
			configPath = os.Args[4]
		}
	} else if len(os.Args) >= 3 && (os.Args[1] == "-c" || os.Args[1] == "--config") {
		// Handles standard execution: go run cmd/migration/main.go -c ./cmd/migration/mongo.yaml
		configPath = os.Args[2]
	}

	if configPath == "" {
		logger.Fatalf("Usage:\n  Generate: go run cmd/migration/main.go generate <name> -c <path>\n  Migrate:  go run cmd/migration/main.go -c <path>")
	}

	// 2. Load the YAML configuration first, regardless of the command
	migConfig, err := migrations.LoadConfig(configPath)
	if err != nil {
		logger.Fatalf("Failed to load mongo.yaml: %v", err)
	}

	// 3. Execute the generator if requested and exit early
	// Check what we are generating
	if isGenerate {
		if migrationName == "queries" {
			// GENERATE QUERIES (like sqlc)
			if err := migrations.GenerateQueries(migConfig); err != nil {
				logger.Fatalf("Code generation failed: %v", err)
			}
			logger.Println("Successfully generated code for MongoDB collections")
		} else {
			// GENERATE MIGRATION FILE (like goose)
			if err := migrations.GenerateMigrationFile(migConfig, migrationName); err != nil {
				logger.Fatalf("Generation failed: %v", err)
			}
			logger.Printf("Successfully generated migration: %s\n", migrationName)
		}
		return
	}

	// 4. Connect to Database for running standard migrations
	appCfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatalf("Failed to load app config: %v", err)
	}

	ctx := context.Background()
	dbClient, err := mongodb.New(ctx, &appCfg.DB)
	if err != nil {
		logger.Fatalf("Failed to connect: %v", err)
	}
	defer dbClient.Close(ctx)

	migrator := migrations.NewMigration(dbClient.Database(), logger, migConfig)

	if err := migrator.RunMigrations(); err != nil {
		logger.Fatalf("Migration failed: %v", err)
	}

	logger.Println("Database setup complete.")
}
