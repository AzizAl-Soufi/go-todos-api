package ratelimit

import (
	"fmt"
	"sync/atomic"
	"testing"
)

// BenchmarkAllow measures the limiter POST /todos now passes through. The
// budget is large enough that every request is allowed, the common case.
func BenchmarkAllow(b *testing.B) {
	b.Run("one user", func(b *testing.B) {
		l := New(1<<30, 1<<30)
		for b.Loop() {
			l.Allow("user-1")
		}
	})

	b.Run("10k users", func(b *testing.B) {
		l := New(1<<30, 1<<30)
		keys := make([]string, 10_000)
		for i := range keys {
			keys[i] = fmt.Sprintf("user-%d", i)
		}
		i := 0
		for b.Loop() {
			l.Allow(keys[i%len(keys)])
			i++
		}
	})

	// All goroutines share the limiter's one mutex: this is the contention
	// concurrent POST /todos requests see.
	b.Run("parallel", func(b *testing.B) {
		l := New(1<<30, 1<<30)
		keys := make([]string, 1_000)
		for i := range keys {
			keys[i] = fmt.Sprintf("user-%d", i)
		}
		var next atomic.Int64
		b.RunParallel(func(pb *testing.PB) {
			i := int(next.Add(1))
			for pb.Next() {
				l.Allow(keys[i%len(keys)])
				i++
			}
		})
	})
}
