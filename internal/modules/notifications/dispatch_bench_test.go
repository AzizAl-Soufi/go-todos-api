package notifications

import (
	"context"
	"testing"
)

// BenchmarkBackground_Run measures what starting a notification adds to the
// request that adds a team task: the lock, a slot, a goroutine. The limit is
// high enough that nothing is dropped (dropping is cheaper and would flatter
// the result).
func BenchmarkBackground_Run(b *testing.B) {
	bg := newBackground(1 << 20)
	b.ReportAllocs()
	for b.Loop() {
		bg.run(func() {})
	}
	if err := bg.close(context.Background()); err != nil {
		b.Fatal(err)
	}
}
