// Package storetest is the contract every job store must satisfy; the
// memory, MongoDB and PostgreSQL stores all run it.
package storetest

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
)

// Stores is one backend's job store on empty storage, plus a well-formed ID
// nothing has.
type Stores struct {
	Store     jobs.Store
	MissingID string
}

func Run(t *testing.T, newStores func(t *testing.T) Stores) {
	t.Run("EnqueueAndGet", func(t *testing.T) { testEnqueue(t, newStores(t)) })
	t.Run("Claim", func(t *testing.T) { testClaim(t, newStores(t)) })
	t.Run("ConcurrentClaims", func(t *testing.T) { testConcurrentClaims(t, newStores(t)) })
	t.Run("Outcomes", func(t *testing.T) { testOutcomes(t, newStores(t)) })
	t.Run("Leases", func(t *testing.T) { testLeases(t, newStores(t)) })
}

var ctx = context.Background()

// now is truncated to what every backend stores (MongoDB keeps milliseconds).
func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func enqueue(t *testing.T, s jobs.Store, kind string, runAt time.Time, key string) *jobs.Job {
	t.Helper()
	at := now()
	j := &jobs.Job{
		Kind: kind, Payload: []byte(`{"n":1}`), UniqueKey: key, OwnerID: "owner-1", Status: jobs.Queued,
		MaxAttempts: 3, RunAt: runAt, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.Enqueue(ctx, j); err != nil || j.ID == "" {
		t.Fatalf("enqueue %s: %v, id %q", kind, err, j.ID)
	}
	return j
}

func testEnqueue(t *testing.T, s Stores) {
	at := now()
	j := enqueue(t, s.Store, "mail.send", at, "")
	got, err := s.Store.Get(ctx, j.ID)
	if err != nil || got.Kind != "mail.send" || string(got.Payload) != `{"n": 1}` && string(got.Payload) != `{"n":1}` ||
		got.Status != jobs.Queued || got.Attempts != 0 || got.MaxAttempts != 3 || got.OwnerID != "owner-1" ||
		!got.RunAt.Equal(at) || !got.FinishedAt.IsZero() {
		t.Fatalf("round trip: %+v (%v)", got, err)
	}

	first := enqueue(t, s.Store, "schedule.tick", at, "slot-1")
	again := enqueue(t, s.Store, "schedule.tick", at, "slot-1")
	if again.ID != first.ID {
		t.Fatalf("a unique key enqueued twice: %s and %s", first.ID, again.ID)
	}
	if other := enqueue(t, s.Store, "schedule.tick", at, "slot-2"); other.ID == first.ID {
		t.Fatal("different unique keys share a job")
	}

	if _, err := s.Store.Get(ctx, s.MissingID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("missing job: %v", err)
	}
	if _, err := s.Store.Get(ctx, "not-an-id"); !errors.Is(err, apperrors.ErrInvalidID) {
		t.Fatalf("malformed ID: %v", err)
	}
}

func ids(list []*jobs.Job) []string {
	out := []string{}
	for _, j := range list {
		out = append(out, j.ID)
	}
	return out
}

func testClaim(t *testing.T, s Stores) {
	at := now()
	late := enqueue(t, s.Store, "a", at.Add(-time.Minute), "")
	early := enqueue(t, s.Store, "b", at.Add(-time.Hour), "")
	future := enqueue(t, s.Store, "c", at.Add(time.Hour), "")

	got, err := s.Store.Claim(ctx, jobs.Claim{Now: at, Lease: time.Minute, Limit: 10, CountAttempt: true})
	if err != nil || !slices.Equal(ids(got), []string{early.ID, late.ID}) {
		t.Fatalf("Claim = %v (%v), want the due jobs, earliest first", ids(got), err)
	}
	if j := got[0]; j.Status != jobs.Running || j.Attempts != 1 || !j.LockedUntil.Equal(at.Add(time.Minute)) {
		t.Fatalf("claimed job: %+v", j)
	}
	if again, _ := s.Store.Claim(ctx, jobs.Claim{Now: at, Lease: time.Minute, Limit: 10, CountAttempt: true}); len(again) != 0 {
		t.Fatalf("leased jobs claimed again: %v", ids(again))
	}

	// A lease that ran out (its worker died) is claimed again.
	later := at.Add(2 * time.Minute)
	again, err := s.Store.Claim(ctx, jobs.Claim{Now: later, Lease: time.Minute, Limit: 1, CountAttempt: false})
	if err != nil || len(again) != 1 || again[0].ID != early.ID || again[0].Attempts != 1 {
		t.Fatalf("expired lease: %v (%v), want %s without counting an attempt", ids(again), err, early.ID)
	}
	all, _ := s.Store.Claim(ctx, jobs.Claim{Now: at.Add(2 * time.Hour), Lease: time.Minute, Limit: 10, CountAttempt: true})
	if !slices.Contains(ids(all), future.ID) {
		t.Fatalf("future job not claimed once due: %v", ids(all))
	}
}

func testConcurrentClaims(t *testing.T, s Stores) {
	at := now()
	const total = 20
	for range total {
		enqueue(t, s.Store, "k", at, "")
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for {
				got, err := s.Store.Claim(ctx, jobs.Claim{Now: at, Lease: time.Minute, Limit: 3, CountAttempt: true})
				if err != nil {
					t.Error(err)
					return
				}
				if len(got) == 0 {
					return
				}
				mu.Lock()
				for _, j := range got {
					seen[j.ID]++
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(seen) != total {
		t.Fatalf("claimed %d distinct jobs, want %d", len(seen), total)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("job %s claimed %d times", id, n)
		}
	}
}

func testOutcomes(t *testing.T, s Stores) {
	at := now()
	ok := enqueue(t, s.Store, "ok", at, "")
	bad := enqueue(t, s.Store, "bad", at, "")
	flaky := enqueue(t, s.Store, "flaky", at, "")
	relayed := enqueue(t, s.Store, "relayed", at, "")
	if _, err := s.Store.Claim(ctx, jobs.Claim{Now: at, Lease: time.Minute, Limit: 10, CountAttempt: true}); err != nil {
		t.Fatal(err)
	}
	done := at.Add(time.Second)
	retryAt := at.Add(time.Hour)
	for name, err := range map[string]error{
		"succeed":   s.Store.Succeed(ctx, ok.ID, done),
		"fail":      s.Store.Fail(ctx, bad.ID, done, "boom"),
		"retry":     s.Store.Retry(ctx, flaky.ID, retryAt, done, "try later"),
		"dispatch":  s.Store.Dispatch(ctx, relayed.ID, done),
		"attempted": s.Store.Attempted(ctx, relayed.ID, 2, done, "redis retry"),
	} {
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	check := func(id string, status jobs.Status, wantErr string, finished bool) *jobs.Job {
		t.Helper()
		j, err := s.Store.Get(ctx, id)
		if err != nil || j.Status != status || j.LastError != wantErr || j.FinishedAt.IsZero() == finished {
			t.Fatalf("job %s: %+v (%v), want %s %q finished=%v", id, j, err, status, wantErr, finished)
		}
		return j
	}
	check(ok.ID, jobs.Succeeded, "", true)
	check(bad.ID, jobs.Failed, "boom", true)
	if j := check(flaky.ID, jobs.Queued, "try later", false); !j.RunAt.Equal(retryAt) {
		t.Fatalf("retry run at %v, want %v", j.RunAt, retryAt)
	}
	if j := check(relayed.ID, jobs.Dispatched, "redis retry", false); j.Attempts != 2 {
		t.Fatalf("dispatched attempts = %d", j.Attempts)
	}
	if got, _ := s.Store.Claim(ctx, jobs.Claim{Now: at.Add(time.Minute), Lease: time.Minute, Limit: 10, CountAttempt: true}); len(got) != 0 {
		t.Fatalf("finished or dispatched jobs claimed: %v", ids(got))
	}

	c, err := s.Store.Counts(ctx, at)
	if err != nil || c.ByStatus[jobs.Succeeded] != 1 || c.ByStatus[jobs.Failed] != 1 || c.ByStatus[jobs.Queued] != 1 ||
		c.ByStatus[jobs.Dispatched] != 1 || c.FailedSince != 1 || !c.OldestQueued.Equal(retryAt) {
		t.Fatalf("Counts = %+v (%v)", c, err)
	}

	n, err := s.Store.DeleteFinished(ctx, done.Add(time.Second))
	if err != nil || n != 2 {
		t.Fatalf("DeleteFinished = %d (%v), want the succeeded and failed job", n, err)
	}
	if _, err := s.Store.Get(ctx, ok.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("finished job not deleted: %v", err)
	}
	if err := s.Store.Succeed(ctx, s.MissingID, done); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("outcome for a missing job: %v", err)
	}
}

func testLeases(t *testing.T, s Stores) {
	at := now()
	acquire := func(holder string, until, now time.Time) bool {
		t.Helper()
		ok, err := s.Store.AcquireLease(ctx, "scheduler", holder, until, now)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !acquire("a", at.Add(30*time.Second), at) {
		t.Fatal("a free lease must be taken")
	}
	if acquire("b", at.Add(30*time.Second), at.Add(time.Second)) {
		t.Fatal("a live lease must not be taken by another holder")
	}
	if !acquire("a", at.Add(60*time.Second), at.Add(10*time.Second)) {
		t.Fatal("the holder renews its lease")
	}
	if !acquire("b", at.Add(2*time.Minute), at.Add(61*time.Second)) {
		t.Fatal("an expired lease must be taken over")
	}
	if err := s.Store.ReleaseLease(ctx, "scheduler", "a"); err != nil {
		t.Fatal(err)
	}
	if acquire("a", at.Add(2*time.Minute), at.Add(62*time.Second)) {
		t.Fatal("releasing someone else's lease must not free it")
	}
	if err := s.Store.ReleaseLease(ctx, "scheduler", "b"); err != nil {
		t.Fatal(err)
	}
	if !acquire("a", at.Add(2*time.Minute), at.Add(63*time.Second)) {
		t.Fatal("a released lease is free")
	}
}
