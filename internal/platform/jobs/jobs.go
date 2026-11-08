// Package jobs runs work outside HTTP requests: durable background jobs and
// periodic schedules.
//
// A job is a row in the job store (memory, PostgreSQL or MongoDB, like every
// other store). Enqueue joins the caller's transaction, so a job exists
// exactly when the change that asked for it was committed: the transactional
// outbox pattern. Jobs then run in the worker process (cmd/worker), either
//
//   - straight from the store (Runner): workers claim due jobs with a lease,
//     run them with a timeout, retry failures with exponential backoff, and
//     give up after MaxAttempts (logged as a dead job); or
//   - through Redis (adapters in jobs/asynqx): a relay hands due jobs to asynq,
//     which runs and retries them; the outcome is written back to the store.
//
// Either way the store is the source of truth for a job's status, which is
// what /operations/{id} reports. Handlers must be idempotent: a job can run
// more than once (a worker crashing after the work, before recording it).
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// Status is where a job is in its life.
type Status string

const (
	Queued     Status = "queued"     // waiting for its RunAt
	Running    Status = "running"    // claimed by a worker until LockedUntil
	Dispatched Status = "dispatched" // handed to Redis (asynq), which runs it
	Succeeded  Status = "succeeded"
	Failed     Status = "failed" // gave up: a permanent error, or out of attempts
)

// Finished reports whether the job will not run again.
func (s Status) Finished() bool { return s == Succeeded || s == Failed }

const (
	DefaultMaxAttempts = 10
	DefaultTimeout     = time.Minute
	maxErrorLength     = 2000
)

// Job is a unit of background work.
type Job struct {
	ID      string
	Kind    string // what to run: a registered handler, e.g. "notifications.todo_created"
	Payload []byte // the handler's input, JSON
	// UniqueKey, when set, makes Enqueue a no-op if a job with the same key
	// was already enqueued (and not yet cleaned up): one job per event or
	// schedule slot.
	UniqueKey string
	// OwnerID is the user who asked for the job, who may read its status
	// (GET /operations/{id}); "" for system jobs.
	OwnerID     string
	Status      Status
	Attempts    int
	MaxAttempts int
	RunAt       time.Time
	LockedUntil time.Time // while Running
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	FinishedAt  time.Time // zero until Succeeded or Failed
}

// ---- errors ----

var ErrJobNotFound = apperrors.NotFound("OPERATION_NOT_FOUND", "operation not found")

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marks a handler error that retrying can't fix (the input is
// invalid, the record is gone): the job fails at once.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err was marked with Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

func truncateError(err error) string {
	msg := err.Error()
	if len(msg) > maxErrorLength {
		msg = msg[:maxErrorLength]
	}
	return msg
}

// ---- handlers ----

// Handler runs one job. It gets the payload as enqueued.
type Handler func(ctx context.Context, payload []byte) error

type registration struct {
	handler Handler
	timeout time.Duration
}

// Registry maps job kinds to their handlers. Modules register theirs at
// startup (Module.RegisterJobs); the worker runs them.
type Registry struct {
	handlers map[string]registration
}

func NewRegistry() *Registry { return &Registry{handlers: map[string]registration{}} }

// Register adds a handler for kind; timeout bounds one run (0: DefaultTimeout).
// Registering a kind twice is a programming error and panics.
func (r *Registry) Register(kind string, timeout time.Duration, h Handler) {
	if _, dup := r.handlers[kind]; dup {
		panic(fmt.Sprintf("jobs: kind %q registered twice", kind))
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	r.handlers[kind] = registration{handler: h, timeout: timeout}
}

// Kinds lists the registered kinds.
func (r *Registry) Kinds() []string {
	out := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		out = append(out, k)
	}
	return out
}

func (r *Registry) lookup(kind string) (registration, bool) {
	reg, ok := r.handlers[kind]
	return reg, ok
}

// Handle registers a typed handler: the payload is decoded into T first, and
// a payload that doesn't decode fails the job permanently.
func Handle[T any](r *Registry, kind string, timeout time.Duration, fn func(ctx context.Context, in T) error) {
	r.Register(kind, timeout, func(ctx context.Context, payload []byte) error {
		var in T
		if err := json.Unmarshal(payload, &in); err != nil {
			return Permanent(fmt.Errorf("decode %s payload: %w", kind, err))
		}
		return fn(ctx, in)
	})
}

// ---- enqueueing ----

// Enqueuer adds jobs. With a context from tx.Runner.WithinTx the job is part
// of that transaction.
type Enqueuer interface {
	Enqueue(ctx context.Context, kind string, payload any, opts ...Option) (id string, err error)
}

type options struct {
	runAt       time.Time
	delay       time.Duration
	uniqueKey   string
	ownerID     string
	maxAttempts int
}

type Option func(*options)

// RunAt delays the job until t.
func RunAt(t time.Time) Option { return func(o *options) { o.runAt = t } }

// Delay delays the job by d from now.
func Delay(d time.Duration) Option { return func(o *options) { o.delay = d } }

// UniqueKey enqueues the job only once per key (see Job.UniqueKey).
func UniqueKey(key string) Option { return func(o *options) { o.uniqueKey = key } }

// Owner records the user who may read the job's status.
func Owner(userID string) Option { return func(o *options) { o.ownerID = userID } }

// MaxAttempts bounds how often the job runs before it fails (default 10).
func MaxAttempts(n int) Option { return func(o *options) { o.maxAttempts = n } }

// Queue is the Enqueuer over a Store.
type Queue struct {
	store Store
	now   func() time.Time
}

var _ Enqueuer = (*Queue)(nil)

func NewQueue(s Store, now func() time.Time) *Queue {
	if now == nil {
		now = time.Now
	}
	return &Queue{store: s, now: now}
}

// Enqueue stores a job. With a UniqueKey that was used before, it returns
// the existing job's ID and adds nothing.
func (q *Queue) Enqueue(ctx context.Context, kind string, payload any, opts ...Option) (string, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("jobs: encode %s payload: %w", kind, err)
	}
	now := q.now().UTC()
	runAt := now.Add(o.delay)
	if !o.runAt.IsZero() {
		runAt = o.runAt.UTC()
	}
	if o.maxAttempts <= 0 {
		o.maxAttempts = DefaultMaxAttempts
	}
	j := &Job{
		Kind: kind, Payload: body, UniqueKey: o.uniqueKey, OwnerID: o.ownerID,
		Status: Queued, MaxAttempts: o.maxAttempts, RunAt: runAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := q.store.Enqueue(ctx, j); err != nil {
		return "", err
	}
	return j.ID, nil
}

// ---- storage ----

// Store keeps jobs and the scheduler's leases. Every backend implements it
// and passes storetest. Calls with a context from tx.Runner.WithinTx join
// that transaction.
type Store interface {
	// Enqueue stores j and assigns j.ID. If j.UniqueKey is set and a job
	// with that key exists, nothing is stored and j.ID is that job's ID.
	Enqueue(ctx context.Context, j *Job) error
	// Claim atomically takes up to c.Limit jobs that are due: queued with
	// RunAt <= c.Now, or running with an expired lease (their worker died).
	// They become running until c.Now + c.Lease, with Attempts + 1 when
	// c.CountAttempt (the relay hands jobs to Redis without counting).
	// Earliest RunAt first.
	Claim(ctx context.Context, c Claim) ([]*Job, error)
	// Succeed, Fail, Retry and Dispatch record an outcome. They return
	// ErrNotFound for an unknown job.
	Succeed(ctx context.Context, id string, at time.Time) error
	Fail(ctx context.Context, id string, at time.Time, errMsg string) error
	// Retry puts the job back in the queue for runAt.
	Retry(ctx context.Context, id string, runAt, at time.Time, errMsg string) error
	// Dispatch marks a job handed to Redis.
	Dispatch(ctx context.Context, id string, at time.Time) error
	// Attempted records a failed run of a dispatched job (Redis retries it).
	Attempted(ctx context.Context, id string, attempts int, at time.Time, errMsg string) error
	Get(ctx context.Context, id string) (*Job, error)
	// DeleteFinished removes jobs that finished before cutoff.
	DeleteFinished(ctx context.Context, before time.Time) (int64, error)
	// Counts reports how many jobs are in each status, and how many failed
	// since `since` (for the worker's health and alerts).
	Counts(ctx context.Context, since time.Time) (Counts, error)

	// AcquireLease takes or renews the lease name for holder until until; it
	// fails (false) while another holder's lease hasn't expired at now.
	AcquireLease(ctx context.Context, name, holder string, until, now time.Time) (bool, error)
	ReleaseLease(ctx context.Context, name, holder string) error
}

// Claim is a Store.Claim request.
type Claim struct {
	Now          time.Time
	Lease        time.Duration
	Limit        int
	CountAttempt bool
}

// Counts is a snapshot of the queue.
type Counts struct {
	ByStatus     map[Status]int64
	FailedSince  int64
	OldestQueued time.Time // zero when nothing is due
}

// Timeout is how long one run of kind may take (DefaultTimeout for unknown
// kinds).
func (r *Registry) Timeout(kind string) time.Duration {
	if reg, ok := r.lookup(kind); ok {
		return reg.timeout
	}
	return DefaultTimeout
}
