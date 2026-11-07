package inMemory

import "sync"

type MemoryClient[K comparable, V any] struct {
	Mu   sync.RWMutex
	Data map[K]*V
}

// Clone returns a shallow copy of v so callers never share the stored value.
// A nil pointer clones to nil.
func Clone[T any](v *T) *T {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
