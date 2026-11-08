package asynqx_test

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/asynqx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/memstore"
)

// Runs against a real Redis when TEST_REDIS_URL is set (make test-integration).
func TestExecutor(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("redis: %v", err)
	}

	store := memstore.New()
	reg := jobs.NewRegistry()
	var okRuns, flakyRuns atomic.Int32
	reg.Register("ok", time.Second, func(context.Context, []byte) error { okRuns.Add(1); return nil })
	reg.Register("flaky", time.Second, func(context.Context, []byte) error {
		if flakyRuns.Add(1) == 1 {
			return errors.New("first run fails")
		}
		return nil
	})
	reg.Register("broken", time.Second, func(context.Context, []byte) error { return jobs.Permanent(errors.New("bad input")) })
	reg.Register("exhausted", time.Second, func(context.Context, []byte) error { return errors.New("always fails") })

	q := jobs.NewQueue(store, nil)
	ctx := context.Background()
	ids := map[string]string{}
	for _, kind := range []string{"ok", "flaky", "broken"} {
		ids[kind], _ = q.Enqueue(ctx, kind, nil)
	}
	ids["exhausted"], _ = q.Enqueue(ctx, "exhausted", nil, jobs.MaxAttempts(2))

	ex, err := asynqx.New(url, store, reg, 4)
	if err != nil {
		t.Fatal(err)
	}
	ex.RetryDelay = func(int) time.Duration { return 100 * time.Millisecond }
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- ex.Run(runCtx) }()

	want := map[string]jobs.Status{"ok": jobs.Succeeded, "flaky": jobs.Succeeded, "broken": jobs.Failed, "exhausted": jobs.Failed}
	deadline := time.Now().Add(30 * time.Second)
	for {
		settled := true
		for kind, status := range want {
			j, _ := store.Get(ctx, ids[kind])
			settled = settled && j.Status == status
		}
		if settled || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for kind, status := range want {
		if j, _ := store.Get(ctx, ids[kind]); j.Status != status {
			t.Errorf("%s: %s (%q), want %s", kind, j.Status, j.LastError, status)
		}
	}
	if okRuns.Load() != 1 || flakyRuns.Load() != 2 {
		t.Errorf("ok ran %d times, flaky %d; want 1 and 2", okRuns.Load(), flakyRuns.Load())
	}
	if j, _ := store.Get(ctx, ids["exhausted"]); j.Attempts != 1 {
		// The second, last attempt fails it outright; the first is recorded.
		t.Errorf("exhausted attempts recorded = %d, want 1", j.Attempts)
	}
}
