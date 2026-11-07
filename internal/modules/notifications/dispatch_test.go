package notifications

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackground_DropsBeyondTheLimit(t *testing.T) {
	b := newBackground(2)
	release := make(chan struct{})
	var ran atomic.Int32
	for range 3 {
		b.run(func() {
			ran.Add(1)
			<-release
		})
	}
	close(release)
	if err := b.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ran.Load(); got != 2 {
		t.Fatalf("ran %d notifications, want 2 (the third dropped)", got)
	}

	// Finished notifications free their slot.
	b = newBackground(1)
	for range 3 {
		done := make(chan struct{})
		b.run(func() { close(done) })
		<-done
		waitForFreeSlot(t, b)
	}
}

func TestBackground_SurvivesPanics(t *testing.T) {
	b := newBackground(1)
	b.run(func() { panic("boom") })
	if err := b.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(b.slots) != 0 {
		t.Fatal("a panicking notification must free its slot")
	}
}

func TestBackground_CloseWaitsForRunningNotifications(t *testing.T) {
	b := newBackground(4)
	release := make(chan struct{})
	finished := make(chan struct{})
	b.run(func() {
		<-release
		close(finished)
	})

	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := b.close(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close with a notification still running: err = %v", err)
	}

	var late atomic.Bool
	b.run(func() { late.Store(true) })

	close(release)
	if err := b.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("close returned before the running notification finished")
	}
	if late.Load() {
		t.Fatal("a notification started after close")
	}
}

func TestModule_CloseWithoutBackground(t *testing.T) {
	m := New(Deps{Dispatch: func(f func()) { f() }})
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// waitForFreeSlot waits until the notification that just finished has
// released its slot (it does so after f returns).
func waitForFreeSlot(t *testing.T, b *background) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(b.slots) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("slot not released")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBackground_LimitBelowOneStillRuns(t *testing.T) {
	b := newBackground(0)
	ran := make(chan struct{})
	b.run(func() { close(ran) })
	if err := b.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	default:
		t.Fatal("a limit of 0 must not drop every notification")
	}
}
