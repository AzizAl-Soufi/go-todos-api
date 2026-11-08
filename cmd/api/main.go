// Command api runs the todos HTTP API.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	cancel() // before os.Exit, which skips deferred calls
	if err != nil {
		slog.Error("api failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	a, err := newApp(ctx, cfg)
	if err != nil {
		return err
	}
	defer a.close()

	return a.run(ctx)
}
