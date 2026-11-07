// Package memstore holds the in-memory audit event store (local development
// and tests). Filtering and ordering reuse domain.Filter and domain.Compare.
package memstore

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Events struct {
	mu     sync.RWMutex
	events []*domain.Event
}

var _ application.EventStore = (*Events)(nil)

func NewEvents() *Events { return &Events{} }

func clone(e *domain.Event) *domain.Event {
	c := *e
	c.Details = maps.Clone(e.Details)
	if c.Details == nil {
		c.Details = map[string]string{}
	}
	return &c
}

func (s *Events) Append(_ context.Context, e *domain.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.ID = bson.NewObjectID().Hex()
	s.events = append(s.events, clone(e))
	return nil
}

func (s *Events) List(_ context.Context, f domain.Filter) ([]*domain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*domain.Event{}
	for _, e := range s.events {
		if f.Matches(e) {
			out = append(out, clone(e))
		}
	}
	slices.SortFunc(out, domain.Compare)
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *Events) deleteWhere(match func(*domain.Event) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = slices.DeleteFunc(s.events, match)
}

func (s *Events) DeleteByUser(_ context.Context, userID string) error {
	s.deleteWhere(func(e *domain.Event) bool { return e.UserID == userID })
	return nil
}

func (s *Events) DeleteByTeam(_ context.Context, teamID string) error {
	s.deleteWhere(func(e *domain.Event) bool { return e.TeamID == teamID })
	return nil
}
