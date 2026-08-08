package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/AzizAl-Soufi/todos-api/internal/common/config"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.LoadConfig()
	if err != nil {
		panic(err)
	}

	api, cleanup := NewApplication(ctx, cfg)

	defer cleanup()

	handler := api.initialize()

	if err := api.run(ctx, handler); err != nil {
		slog.Error("Server has failed to start", "error", err.Error())

		os.Exit(1)
	}
}
