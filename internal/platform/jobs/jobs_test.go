package jobs_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/memstore"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

type fixture struct {
	store  *memstore.Store
	queue  *jobs.Queue
	reg    *jobs.Registry
	runner *jobs.Runner
	clock  *clock
}

func newFixture() *fixture {
	c := &clock{t: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	s := memstore.New()
	reg := jobs.NewRegistry()
	return &fixture{store: s, queue: jobs.NewQueue(s, c.now), reg: reg, runner: jobs.NewRunner(s, reg, jobs.RunnerConfig{}, c.now), clock: c}
}

func (f *fixture) job(t *testing.T, id string) *jobs.Job {
	t.Helper()
	j, err := f.store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestRunner_Outcomes(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	var calls atomic.Int32
	type payload struct{ N int }
	jobs.Handle(f.reg, "ok", 0, func(_ context.Context, p payload) error {
		if p.N != 7 {
			t.Errorf("payload N = %d", p.N)
		}
		calls.Add(1)
		return nil
	})
	f.reg.Register("flaky", 0, func(context.Context, []byte) error { return errors.New("upstream down") })
	f.reg.Register("broken", 0, func(context.Context, []byte) error { return jobs.Permanent(errors.New("bad input")) })
	f.reg.Register("panics", 0, func(context.Context, []byte) error { panic("oops") })

	ok, _ := f.queue.Enqueue(ctx, "ok", payload{N: 7})
	flaky, _ := f.queue.Enqueue(ctx, "flaky", nil, jobs.MaxAttempts(2))
	broken, _ := f.queue.Enqueue(ctx, "broken", nil)
	panics, _ := f.queue.Enqueue(ctx, "panics", nil)
	unknown, _ := f.queue.Enqueue(ctx, "nobody.handles", nil)
	undecodable, _ := f.queue.Enqueue(ctx, "ok", "not an object")
	later, _ := f.queue.Enqueue(ctx, "ok", payload{N: 7}, jobs.Delay(time.Hour))

	if n, err := f.runner.RunDue(ctx); err != nil || n != 6 {
		t.Fatalf("RunDue = %d (%v), want the 6 due jobs", n, err)
	}
	if calls.Load() != 1 || f.job(t, ok).Status != jobs.Succeeded {
		t.Fatalf("ok: %d calls, %+v", calls.Load(), f.job(t, ok))
	}
	for name, id := range map[string]string{"permanent error": broken, "no handler": unknown, "undecodable payload": undecodable} {
		if j := f.job(t, id); j.Status != jobs.Failed || j.LastError == "" {
			t.Errorf("%s: %+v, want failed with the error", name, j)
		}
	}
	if j := f.job(t, panics); j.Status != jobs.Queued || !strings.Contains(j.LastError, "panic: oops") {
		t.Fatalf("panic: %+v, want a retry", j)
	}
	j := f.job(t, flaky)
	if j.Status != jobs.Queued || j.Attempts != 1 || !j.RunAt.After(f.clock.t) || j.LastError != "upstream down" {
		t.Fatalf("flaky after one run: %+v, want queued for later", j)
	}
	if f.job(t, later).Status != jobs.Queued {
		t.Fatal("a delayed job ran early")
	}

	// The second (last) attempt fails it for good.
	f.clock.t = j.RunAt
	if _, err := f.runner.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	if j := f.job(t, flaky); j.Status != jobs.Failed || j.Attempts != 2 {
		t.Fatalf("flaky after its last attempt: %+v", j)
	}
}

func TestRunner_RunsUntilCancelled(t *testing.T) {
	f := newFixture()
	var done atomic.Int32
	f.reg.Register("work", time.Second, func(context.Context, []byte) error {
		done.Add(1)
		return nil
	})
	for range 25 {
		if _, err := f.queue.Enqueue(context.Background(), "work", nil); err != nil {
			t.Fatal(err)
		}
	}
	runner := jobs.NewRunner(f.store, f.reg, jobs.RunnerConfig{Concurrency: 4, PollInterval: 10 * time.Millisecond}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(stopped)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for done.Load() < 25 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-stopped
	if done.Load() != 25 {
		t.Fatalf("%d of 25 jobs ran", done.Load())
	}
}

func TestScheduler(t *testing.T) {
	ctx := context.Background()
	f := newFixture()
	schedules := []jobs.Schedule{{Kind: "cleanup", Every: time.Hour}, {Kind: "alerts", Every: 5 * time.Minute}}
	a := jobs.NewScheduler(f.store, f.queue, schedules, "worker-a", f.clock.now)
	b := jobs.NewScheduler(f.store, f.queue, schedules, "worker-b", f.clock.now)

	if leader, err := a.Tick(ctx); err != nil || !leader {
		t.Fatalf("first scheduler: leader=%v (%v)", leader, err)
	}
	if leader, _ := b.Tick(ctx); leader {
		t.Fatal("two schedulers lead at once")
	}
	f.clock.t = f.clock.t.Add(time.Minute)
	if _, err := a.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	c, _ := f.store.Counts(ctx, time.Time{})
	if c.ByStatus[jobs.Queued] != 2 {
		t.Fatalf("queued %d jobs, want one per schedule slot (no duplicates across ticks)", c.ByStatus[jobs.Queued])
	}

	// The next 5-minute slot adds one alerts job.
	f.clock.t = f.clock.t.Add(5 * time.Minute)
	if _, err := a.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.store.Counts(ctx, time.Time{}); c.ByStatus[jobs.Queued] != 3 {
		t.Fatalf("queued %d after the next slot, want 3", c.ByStatus[jobs.Queued])
	}

	// When a's lease runs out, b takes over.
	f.clock.t = f.clock.t.Add(time.Minute)
	if leader, _ := b.Tick(ctx); !leader {
		t.Fatal("b must take over an expired lease")
	}
}

func TestBackoff(t *testing.T) {
	if d := jobs.Backoff(1); d < 30*time.Second || d > 36*time.Second {
		t.Fatalf("first backoff %v", d)
	}
	if d := jobs.Backoff(50); d < time.Hour || d > 72*time.Minute {
		t.Fatalf("backoff is capped at an hour (plus jitter): %v", d)
	}
}
