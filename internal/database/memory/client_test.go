package inMemory

import (
	"strconv"
	"sync"
	"testing"
)

type item struct{ N int }

func TestMemoryClient_ConcurrentAccess(t *testing.T) {
	c := &MemoryClient[string, item]{Data: make(map[string]*item)}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.Mu.Lock()
			c.Data[strconv.Itoa(i)] = &item{N: i}
			c.Mu.Unlock()
		}()
		go func() {
			defer wg.Done()
			c.Mu.RLock()
			_ = c.Data[strconv.Itoa(i)]
			c.Mu.RUnlock()
		}()
	}
	wg.Wait()

	if len(c.Data) != 100 {
		t.Fatalf("len(Data) = %d, want 100", len(c.Data))
	}
	if c.Data["42"].N != 42 {
		t.Fatalf("Data[42].N = %d, want 42", c.Data["42"].N)
	}
}
