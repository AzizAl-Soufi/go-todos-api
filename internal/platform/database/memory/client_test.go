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

func TestClone(t *testing.T) {
	if Clone[item](nil) != nil {
		t.Fatal("Clone(nil) should be nil")
	}

	orig := &item{N: 1}
	c := Clone(orig)
	if c == orig || *c != *orig {
		t.Fatalf("Clone = %p %+v, want a distinct copy of %p %+v", c, *c, orig, *orig)
	}
	c.N = 2
	if orig.N != 1 {
		t.Fatal("mutating the clone changed the original")
	}
}
