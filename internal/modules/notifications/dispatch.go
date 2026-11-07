package notifications

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
)

// maxInFlight bounds the notifications being sent at once. Each holds a
// database connection and an FCM request for up to sendTimeout, so a burst of
// new tasks or a slow FCM must not pile them up without limit.
const maxInFlight = 32

// background runs notifications in goroutines, at most limit at a time.
// When all slots are busy a notification is dropped and logged: delivery is
// best effort, and adding a task must never wait for a push. A panic in a
// notification is logged instead of crashing the server. close stops new
// notifications and waits for those in flight.
type background struct {
	slots chan struct{}

	mu     sync.Mutex // guards closed and wg.Add, so close can't race a start
	closed bool
	wg     sync.WaitGroup
}

// newBackground runs at most limit notifications at once; a limit below 1
// would drop every notification, so it is raised to 1.
func newBackground(limit int) *background {
	return &background{slots: make(chan struct{}, max(limit, 1))}
}

// run starts f unless the limit is reached or close was called; it never
// blocks.
func (b *background) run(f func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		slog.Warn("notification dropped: shutting down")
		return
	}
	select {
	case b.slots <- struct{}{}:
	default:
		slog.Warn("notification dropped: too many being sent", "limit", cap(b.slots))
		return
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer func() { <-b.slots }()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("notification panicked", "panic", r, "stack", string(debug.Stack()))
			}
		}()
		f()
	}()
}

// close refuses new notifications and waits for the running ones, until ctx
// ends; then it returns ctx's error and the rest keep running unwatched.
func (b *background) close(ctx context.Context) error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
