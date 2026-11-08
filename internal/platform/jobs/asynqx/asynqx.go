// Package asynqx runs jobs through Redis with asynq, instead of straight
// from the database (jobs.Runner).
//
// The database stays the outbox and the source of truth: jobs are enqueued
// there, in the caller's transaction. A relay hands due jobs to asynq, with
// the job's ID as the task ID, so handing one over twice (a crash between
// the hand-over and recording it) is harmless: asynq refuses the duplicate.
// The asynq server runs the same handlers, retries with the same backoff,
// and writes each outcome back to the database, so /operations shows the
// same thing in either mode; asynqmon shows the Redis side.
package asynqx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hibiken/asynq"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
)

const (
	relayBatch    = 100
	relayInterval = time.Second
	// relayLease is how long a relay holds jobs it is handing over.
	relayLease = time.Minute
	// retention keeps finished tasks visible in asynqmon for a day.
	retention = 24 * time.Hour
)

// Executor relays jobs to Redis and runs them there.
type Executor struct {
	// RetryDelay is the wait before retry n+1 (jobs.Backoff by default;
	// tests shorten it).
	RetryDelay  func(n int) time.Duration
	store       jobs.Store
	registry    *jobs.Registry
	redis       asynq.RedisConnOpt
	concurrency int
	now         func() time.Time
}

// New connects to redisURL (redis://[:password@]host:port[/db], or
// rediss:// for TLS).
func New(redisURL string, store jobs.Store, registry *jobs.Registry, concurrency int) (*Executor, error) {
	opt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL: %w", err)
	}
	if concurrency <= 0 {
		concurrency = 8
	}
	return &Executor{RetryDelay: jobs.Backoff, store: store, registry: registry, redis: opt, concurrency: concurrency, now: time.Now}, nil
}

// Run relays and runs jobs until ctx ends, then lets running tasks finish.
func (e *Executor) Run(ctx context.Context) error {
	client := asynq.NewClient(e.redis)
	defer client.Close()

	server := asynq.NewServer(e.redis, asynq.Config{
		Concurrency: e.concurrency,
		RetryDelayFunc: func(n int, _ error, _ *asynq.Task) time.Duration {
			return e.RetryDelay(n + 1)
		},
		ShutdownTimeout: 30 * time.Second,
		LogLevel:        asynq.WarnLevel,
	})
	mux := asynq.NewServeMux()
	for _, kind := range e.registry.Kinds() {
		mux.HandleFunc(kind, e.handle)
	}
	if err := server.Start(mux); err != nil {
		return fmt.Errorf("start asynq server: %w", err)
	}

	var wg sync.WaitGroup
	wg.Go(func() { e.relay(ctx, client) })
	<-ctx.Done()
	wg.Wait()
	server.Shutdown() // waits for running tasks, up to ShutdownTimeout
	return nil
}

// relay hands due jobs to asynq until ctx ends.
func (e *Executor) relay(ctx context.Context, client *asynq.Client) {
	for {
		n, err := e.RelayDue(ctx, client)
		if err != nil && ctx.Err() == nil {
			slog.Error("jobs: relay to redis failed", "error", err)
		}
		if n == relayBatch {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(relayInterval):
		}
	}
}

// RelayDue hands one batch of due jobs to asynq and reports how many it
// claimed.
func (e *Executor) RelayDue(ctx context.Context, client *asynq.Client) (int, error) {
	now := e.now()
	due, err := e.store.Claim(ctx, jobs.Claim{Now: now, Lease: relayLease, Limit: relayBatch, CountAttempt: false})
	if err != nil {
		return 0, err
	}
	for _, j := range due {
		task := asynq.NewTask(j.Kind, j.Payload)
		_, err := client.EnqueueContext(ctx, task,
			asynq.TaskID(j.ID),
			asynq.MaxRetry(max(j.MaxAttempts-1, 0)),
			asynq.Timeout(e.registry.Timeout(j.Kind)),
			asynq.Retention(retention),
		)
		switch {
		case err == nil, errors.Is(err, asynq.ErrTaskIDConflict): // handed over (now, or before a crash)
			if err := e.store.Dispatch(ctx, j.ID, e.now()); err != nil {
				slog.Error("jobs: recording a hand-over to redis failed", "job_id", j.ID, "error", err)
			}
		default:
			// Redis is unreachable: back to the queue, without using an attempt.
			if err := e.store.Retry(ctx, j.ID, e.now().Add(5*time.Second), e.now(), "redis: "+err.Error()); err != nil {
				slog.Error("jobs: requeueing after a redis failure failed", "job_id", j.ID, "error", err)
			}
		}
	}
	return len(due), nil
}

// handle runs one task and records the outcome in the store.
func (e *Executor) handle(ctx context.Context, t *asynq.Task) error {
	id, _ := asynq.GetTaskID(ctx)
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	log := slog.With("job_id", id, "kind", t.Type(), "attempt", retried+1)

	err := e.registry.Execute(ctx, t.Type(), t.Payload())
	now := e.now()
	switch {
	case err == nil:
		if err := e.store.Succeed(ctx, id, now); err != nil {
			log.Error("jobs: recording success failed", "error", err)
		}
		return nil
	case jobs.IsPermanent(err) || retried >= maxRetry:
		if err := e.store.Fail(ctx, id, now, err.Error()); err != nil {
			log.Error("jobs: recording failure failed", "error", err)
		}
		log.Error("jobs: job failed permanently", "error", err)
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	default:
		if err := e.store.Attempted(ctx, id, retried+1, now, err.Error()); err != nil {
			log.Error("jobs: recording the attempt failed", "error", err)
		}
		log.Warn("jobs: job failed, will retry", "error", err)
		return err
	}
}
