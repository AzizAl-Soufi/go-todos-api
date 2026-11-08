package jobs

import (
	"context"
	"log/slog"
	"strconv"
	"time"
)

// Schedule is a periodic job: Kind is enqueued once per Every, at the start
// of each period (aligned to the Unix epoch, so every instance agrees on
// the slots).
type Schedule struct {
	Kind    string
	Every   time.Duration
	Payload any // nil means {}
}

const (
	schedulerLease = "scheduler"
	leaseTTL       = 30 * time.Second
	tickInterval   = 10 * time.Second
)

// Scheduler enqueues periodic jobs. Any number of worker processes can run
// one: they compete for a lease in the store, and only the holder enqueues.
// Each slot's job has a unique key, so a slot is never enqueued twice, even
// across a change of leader.
type Scheduler struct {
	store     Store
	queue     Enqueuer
	schedules []Schedule
	holder    string
	now       func() time.Time
}

// NewScheduler makes a scheduler; holder names this process in the lease
// (e.g. hostname and PID).
func NewScheduler(s Store, q Enqueuer, schedules []Schedule, holder string, now func() time.Time) *Scheduler {
	if now == nil {
		now = time.Now
	}
	return &Scheduler{store: s, queue: q, schedules: schedules, holder: holder, now: now}
}

// Run enqueues due slots until ctx ends, then gives up the lease.
func (s *Scheduler) Run(ctx context.Context) {
	defer func() {
		// Let another instance take over at once rather than after the TTL.
		if err := s.store.ReleaseLease(context.WithoutCancel(ctx), schedulerLease, s.holder); err != nil {
			slog.Warn("jobs: releasing the scheduler lease failed", "error", err)
		}
	}()
	for {
		if _, err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("jobs: scheduler tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(tickInterval):
		}
	}
}

// Tick takes or renews the lease and, as leader, enqueues the current slot
// of every schedule. It reports whether this instance is the leader.
func (s *Scheduler) Tick(ctx context.Context) (bool, error) {
	now := s.now().UTC()
	leader, err := s.store.AcquireLease(ctx, schedulerLease, s.holder, now.Add(leaseTTL), now)
	if err != nil || !leader {
		return false, err
	}
	for _, sch := range s.schedules {
		slot := now.Truncate(sch.Every)
		payload := sch.Payload
		if payload == nil {
			payload = struct{}{}
		}
		key := "schedule:" + sch.Kind + ":" + strconv.FormatInt(slot.Unix(), 10)
		// A missed slot is not caught up: periodic jobs work on the state
		// they find, so the next slot covers it.
		if _, err := s.queue.Enqueue(ctx, sch.Kind, payload, UniqueKey(key), RunAt(slot), MaxAttempts(3)); err != nil {
			return true, err
		}
	}
	return true, nil
}
