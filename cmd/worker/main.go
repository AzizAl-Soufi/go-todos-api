// Command worker runs the background jobs and periodic schedules of the
// todos API: notifications, due-date alerts, cleanup, account and team
// deletion. Run as many as needed next to the API; they share the work
// through the database (or Redis with REDIS_URL), and one of them at a time
// runs the scheduler.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/app"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		slog.Error("worker failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	if cfg.DB.DBType == "memory" {
		return errors.New("DB_TYPE=memory keeps jobs inside the API process: run the API with JOBS_RUN_IN_API instead of a worker")
	}

	store, err := app.NewStorage(ctx, cfg)
	if err != nil {
		return err
	}
	defer store.Close()
	signIn, err := app.NewSignIn(ctx, cfg)
	if err != nil {
		return err
	}
	pusher, err := app.NewPusher(ctx, cfg)
	if err != nil {
		return err
	}
	modules := app.Wire(store, signIn, app.Push{Pusher: pusher}, app.NewMailers(cfg), cfg)
	worker, err := app.NewWorker(store, modules, cfg)
	if err != nil {
		return err
	}

	if cfg.Jobs.HealthPort > 0 {
		health := &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.Jobs.HealthPort),
			Handler:           worker.Health(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			if err := health.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				slog.Error("worker health server failed", "error", err)
			}
		}()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = health.Shutdown(shutdownCtx)
		}()
	}

	// Run returns once ctx ends (SIGTERM) and the running jobs have finished.
	worker.Run(ctx)
	return nil
}
