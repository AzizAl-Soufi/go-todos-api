// Package memstore holds the in-memory device store (local dev, tests).
// Values are copied on the way in and out.
package memstore

import (
	"cmp"
	"context"
	"slices"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	inmem "github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/memory"
)

// Devices are keyed by token.
type Devices struct {
	inmem.MemoryClient[string, domain.Device]
}

var _ application.DeviceRepository = (*Devices)(nil)

func NewDevices() *Devices {
	return &Devices{MemoryClient: inmem.MemoryClient[string, domain.Device]{Data: map[string]*domain.Device{}}}
}

func (s *Devices) Save(_ context.Context, d *domain.Device) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	stored := inmem.Clone(d)
	if existing, ok := s.Data[d.Token]; ok {
		stored.CreatedAt = existing.CreatedAt
	}
	s.Data[d.Token] = stored
	return nil
}

func byRegistration(a, b *domain.Device) int {
	return cmp.Or(a.UpdatedAt.Compare(b.UpdatedAt), cmp.Compare(a.Token, b.Token))
}

func (s *Devices) list(match func(*domain.Device) bool) []*domain.Device {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	out := []*domain.Device{}
	for _, d := range s.Data {
		if match(d) {
			out = append(out, inmem.Clone(d))
		}
	}
	slices.SortFunc(out, byRegistration)
	return out
}

func (s *Devices) ListForUser(_ context.Context, userID string) ([]*domain.Device, error) {
	return s.list(func(d *domain.Device) bool { return d.UserID == userID }), nil
}

func (s *Devices) ListForUsers(_ context.Context, userIDs []string) ([]*domain.Device, error) {
	return s.list(func(d *domain.Device) bool { return slices.Contains(userIDs, d.UserID) }), nil
}

func (s *Devices) Delete(_ context.Context, userID, token string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	if d, ok := s.Data[token]; !ok || d.UserID != userID {
		return apperrors.ErrNotFound
	}
	delete(s.Data, token)
	return nil
}

func (s *Devices) DeleteTokens(_ context.Context, tokens []string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	for _, t := range tokens {
		delete(s.Data, t)
	}
	return nil
}

func (s *Devices) DeleteForUser(_ context.Context, userID string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	for t, d := range s.Data {
		if d.UserID == userID {
			delete(s.Data, t)
		}
	}
	return nil
}
