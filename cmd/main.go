package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/AzizAl-Soufi/todos-api/internal/core/config"
	"github.com/AzizAl-Soufi/todos-api/internal/pkg/mongodb"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.LoadConfig()
	if err != nil {
		panic(err)
	}
	uri := cfg.MongoURI
	if uri == "" {
		log.Fatal("Set your mongodb connection string in environment variables.")
	}

	db, err := mongodb.New(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}

	defer func() {
		if err := db.Close(ctx); err != nil {
			log.Printf("Error disconnecting from MongoDB: %v", err)
		}
	}()

	if err := db.Ping(ctx); err != nil {
		log.Fatalf("Failed to ping MongoDB: %v", err)
	}

	api := application{
		config: cfg,
		db:     db,
	}

	if err := api.run(ctx, api.initialize()); err != nil {
		slog.Error("Server has failed to start", "error", err.Error())

		os.Exit(1)
	}
}
