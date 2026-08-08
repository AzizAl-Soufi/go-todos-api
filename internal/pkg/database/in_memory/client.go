package inMemory

import "sync"

type InMemoryClient[K comparable, V any] struct {
	Mu   sync.RWMutex
	Data map[K]*V
}
