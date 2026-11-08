package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"
)

// RunnerConfig tunes a Runner. Zero values take the defaults.
type RunnerConfig struct {
	// Concurrency is how many jobs run at once (default 8).
	Concurrency int
	// PollInterval is how long an idle runner waits before looking for due
	// jobs again (default 1s).
	PollInterval time.Duration
}

// Runner runs jobs straight from the store. Several runners (worker
// processes) can share a store: claiming is atomic, and a job whose worker
// died is claimed again once its lease expires.
type Runner struct {
	store    Store
	registry *Registry
	cfg      RunnerConfig
	now      func() time.Time
}

func NewRunner(s Store, r *Registry, cfg RunnerConfig, now func() time.Time) *Runner {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &Runner{store: s, registry: r, cfg: cfg, now: now}
}

// Run processes jobs until ctx ends, then waits for the jobs it started.
func (r *Runner) Run(ctx context.Context) {
	slots := make(chan struct{}, r.cfg.Concurrency)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		free := r.cfg.Concurrency - len(slots)
		claimed := 0
		if free > 0 {
			jobs, err := r.claim(ctx, free)
			if err != nil && ctx.Err() == nil {
				slog.Error("jobs: claim failed", "error", err)
			}
			claimed = len(jobs)
			for _, j := range jobs {
				slots <- struct{}{}
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-slots }()
					// A job finishes even when shutdown starts mid-run.
					r.process(context.WithoutCancel(ctx), j)
				}()
			}
		}
		if claimed == free && free > 0 {
			continue // there may be more due right away
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.cfg.PollInterval):
		}
	}
}

// RunDue processes every job due now, one at a time, and returns how many
// ran. For tests and tools; production uses Run.
func (r *Runner) RunDue(ctx context.Context) (int, error) {
	ran := 0
	for {
		jobs, err := r.claim(ctx, 1)
		if err != nil || len(jobs) == 0 {
			return ran, err
		}
		r.process(ctx, jobs[0])
		ran++
	}
}

func (r *Runner) claim(ctx context.Context, limit int) ([]*Job, error) {
	return r.store.Claim(ctx, Claim{Now: r.now(), Lease: r.maxTimeout() + time.Minute, Limit: limit, CountAttempt: true})
}

// maxTimeout is the longest registered timeout: a claim's lease must
// outlast any run, or a slow job would be claimed again while it runs.
func (r *Runner) maxTimeout() time.Duration {
	longest := DefaultTimeout
	for _, reg := range r.registry.handlers {
		longest = max(longest, reg.timeout)
	}
	return longest
}

func (r *Runner) process(ctx context.Context, j *Job) {
	log := slog.With("job_id", j.ID, "kind", j.Kind, "attempt", j.Attempts)
	if j.Attempts > j.MaxAttempts {
		// Its worker died on the last attempt.
		r.record(ctx, log, r.store.Fail(ctx, j.ID, r.now(), "out of attempts"))
		log.Error("jobs: job failed permanently", "error", "out of attempts")
		return
	}
	reg, ok := r.registry.lookup(j.Kind)
	if !ok {
		r.record(ctx, log, r.store.Fail(ctx, j.ID, r.now(), "no handler for kind "+j.Kind))
		log.Error("jobs: job failed permanently", "error", "no handler")
		return
	}

	start := r.now()
	err := run(ctx, reg, j.Payload)
	switch {
	case err == nil:
		r.record(ctx, log, r.store.Succeed(ctx, j.ID, r.now()))
		log.Info("jobs: job succeeded", "duration_ms", r.now().Sub(start).Milliseconds())
	case IsPermanent(err) || j.Attempts >= j.MaxAttempts:
		r.record(ctx, log, r.store.Fail(ctx, j.ID, r.now(), truncateError(err)))
		// Alerting hooks on this line: a job that will never run again.
		log.Error("jobs: job failed permanently", "error", err)
	default:
		next := r.now().Add(Backoff(j.Attempts))
		r.record(ctx, log, r.store.Retry(ctx, j.ID, next, r.now(), truncateError(err)))
		log.Warn("jobs: job failed, will retry", "error", err, "retry_at", next)
	}
}

// run calls the handler with its timeout, turning a panic into an error.
func run(ctx context.Context, reg registration, payload []byte) (err error) {
	ctx, cancel := context.WithTimeout(ctx, reg.timeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v\n%s", p, debug.Stack())
		}
	}()
	return reg.handler(ctx, payload)
}

func (r *Runner) record(_ context.Context, log *slog.Logger, err error) {
	if err != nil {
		// The lease runs out and the job is claimed again: handlers are
		// idempotent, so running it twice is safe.
		log.Error("jobs: recording the outcome failed", "error", err)
	}
}

// Backoff is the wait before attempt n+1: 30s doubling per attempt, at most
// an hour, with up to 20% jitter so failing jobs don't retry in lockstep.
func Backoff(attempt int) time.Duration {
	d := 30 * time.Second
	for range min(max(attempt-1, 0), 10) {
		d *= 2
	}
	d = min(d, time.Hour)
	return d + time.Duration(rand.Int64N(int64(d/5)+1)) //nolint:gosec // G404: retry jitter, not a secret
}

// ErrNoHandler is a job whose kind nothing registered.
var ErrNoHandler = errors.New("jobs: no handler registered")

// Execute runs a job's handler outside the Runner (the asynq server does
// this): payload in, the handler's error out, with its timeout and panic
// recovery.
func (r *Registry) Execute(ctx context.Context, kind string, payload []byte) error {
	reg, ok := r.lookup(kind)
	if !ok {
		return Permanent(fmt.Errorf("%w for %q", ErrNoHandler, kind))
	}
	return run(ctx, reg, payload)
}
