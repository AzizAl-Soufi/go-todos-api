package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/asynqx"
)

// jobPurge deletes finished jobs past their retention.
const jobPurge = "jobs.purge"

// Jobs collects every module's job handlers and periodic schedules.
func Jobs(store *Storage, m Modules, cfg *config.Config) (*jobs.Registry, []jobs.Schedule) {
	r := jobs.NewRegistry()
	m.Notifications.RegisterJobs(r)
	m.Teams.RegisterJobs(r)
	m.Identity.RegisterJobs(r)
	m.OAuth.RegisterJobs(r)
	m.Todos.RegisterJobs(r)

	retain := time.Duration(max(cfg.Jobs.RetainDays, 1)) * 24 * time.Hour
	r.Register(jobPurge, 5*time.Minute, func(ctx context.Context, _ []byte) error {
		n, err := store.Jobs.DeleteFinished(ctx, time.Now().Add(-retain))
		if n > 0 {
			slog.Info("jobs: purged finished jobs", "count", n)
		}
		return err
	})
	schedules := []jobs.Schedule{{Kind: jobPurge, Every: time.Hour}}
	schedules = append(schedules, m.Teams.Schedules()...)
	schedules = append(schedules, m.OAuth.Schedules()...)
	schedules = append(schedules, m.Todos.Schedules()...)
	return r, schedules
}

// Worker runs background jobs and periodic schedules: the job of the
// cmd/worker process (and of the API process with JOBS_RUN_IN_API).
type Worker struct {
	store     *Storage
	runner    *jobs.Runner
	redis     *asynqx.Executor // nil: jobs run straight from the database
	scheduler *jobs.Scheduler
	cfg       *config.Config
}

func NewWorker(store *Storage, m Modules, cfg *config.Config) (*Worker, error) {
	registry, schedules := Jobs(store, m, cfg)
	queue := jobs.NewQueue(store.Jobs, nil)
	w := &Worker{
		store:     store,
		runner:    jobs.NewRunner(store.Jobs, registry, jobs.RunnerConfig{Concurrency: cfg.Jobs.Concurrency}, nil),
		scheduler: jobs.NewScheduler(store.Jobs, queue, schedules, holderName(), nil),
		cfg:       cfg,
	}
	if cfg.Jobs.RedisURL != "" {
		executor, err := asynqx.New(cfg.Jobs.RedisURL, store.Jobs, registry, cfg.Jobs.Concurrency)
		if err != nil {
			return nil, err
		}
		w.redis = executor
	}
	return w, nil
}

// holderName identifies this process in the scheduler lease.
func holderName() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}

// Run processes jobs and schedules until ctx ends, then waits for running
// jobs to finish.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Go(func() { w.scheduler.Run(ctx) })
	mode := "database"
	if w.redis != nil {
		mode = "redis"
		wg.Go(func() {
			if err := w.redis.Run(ctx); err != nil {
				slog.Error("worker: redis executor stopped", "error", err)
			}
		})
	} else {
		wg.Go(func() { w.runner.Run(ctx) })
	}
	slog.Info("worker started", "mode", mode, "concurrency", w.cfg.Jobs.Concurrency, "db", w.cfg.DB.DBType)
	wg.Wait()
	slog.Info("worker stopped")
}

// RunSchedules enqueues the current slot of every periodic job, as the
// scheduler's leader does (tests).
func (w *Worker) RunSchedules(ctx context.Context) error {
	_, err := w.scheduler.Tick(ctx)
	return err
}

// RunDue runs every job that is due now, in place (tests).
func (w *Worker) RunDue(ctx context.Context) (int, error) { return w.runner.RunDue(ctx) }

// Health serves the queue's state for probes and alerting: 200 while the
// store answers, with counts per status, failures in the last hour, and
// how long the oldest due job has waited (a growing backlog means the
// workers can't keep up).
func (w *Worker) Health() http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		now := time.Now()
		c, err := w.store.Jobs.Counts(r.Context(), now.Add(-time.Hour))
		if err != nil {
			slog.Error("worker health check failed", "error", err)
			httpx.RespondJSON(rw, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		lag := 0.0
		if !c.OldestQueued.IsZero() && c.OldestQueued.Before(now) {
			lag = now.Sub(c.OldestQueued).Seconds()
		}
		httpx.RespondJSON(rw, http.StatusOK, map[string]any{
			"status":              "ok",
			"jobs":                c.ByStatus,
			"failedLastHour":      c.FailedSince,
			"oldestDueWaitSecond": lag,
		})
	})
}
