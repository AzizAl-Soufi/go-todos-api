package inMemory

import "sync"

type InMemoryClient[T any] struct {
	Mu   sync.RWMutex
	Data map[string]*T
}
