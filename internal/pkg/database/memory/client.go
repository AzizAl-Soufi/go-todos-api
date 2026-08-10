package inMemory

import "sync"

type MemoryClient[K comparable, V any] struct {
	Mu   sync.RWMutex
	Data map[K]*V
}
