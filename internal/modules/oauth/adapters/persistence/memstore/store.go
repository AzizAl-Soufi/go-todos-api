// Package memstore holds the oauth module's in-memory stores (local dev,
// tests). Values are copied on the way in and out.
package memstore

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ---- clients ----

type Clients struct {
	mu   sync.RWMutex
	data map[string]domain.Client
}

var _ application.ClientRepository = (*Clients)(nil)

func NewClients() *Clients { return &Clients{data: map[string]domain.Client{}} }

func (s *Clients) Create(_ context.Context, c *domain.Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.ID = bson.NewObjectID().Hex()
	stored := *c
	stored.RedirectURIs = slices.Clone(c.RedirectURIs)
	s.data[c.ID] = stored
	return nil
}

func (s *Clients) Get(_ context.Context, id string) (*domain.Client, error) {
	if _, err := ids.ObjectID(id); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.data[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	c.RedirectURIs = slices.Clone(c.RedirectURIs)
	return &c, nil
}

// ---- codes ----

type Codes struct {
	mu   sync.Mutex
	data map[string]domain.Code
}

var _ application.CodeRepository = (*Codes)(nil)

func NewCodes() *Codes { return &Codes{data: map[string]domain.Code{}} }

func (s *Codes) Save(_ context.Context, c *domain.Code) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, old := range s.data {
		if old.ExpiresAt.Before(c.CreatedAt) {
			delete(s.data, hash)
		}
	}
	s.data[c.Hash] = *c
	return nil
}

func (s *Codes) Consume(_ context.Context, hash string) (*domain.Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.data[hash]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	delete(s.data, hash)
	return &c, nil
}

func (s *Codes) DeleteForUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, c := range s.data {
		if c.Owner.UserID == userID {
			delete(s.data, hash)
		}
	}
	return nil
}

// ---- grants ----

type Grants struct {
	mu   sync.RWMutex
	data map[string]domain.Grant
}

var _ application.GrantRepository = (*Grants)(nil)

func NewGrants() *Grants { return &Grants{data: map[string]domain.Grant{}} }

func (s *Grants) Create(_ context.Context, g *domain.Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.data {
		if old.Owner.UserID == g.Owner.UserID && old.RefreshExpiresAt.Before(g.CreatedAt) {
			delete(s.data, id)
		}
	}
	g.ID = bson.NewObjectID().Hex()
	s.data[g.ID] = *g
	return nil
}

func (s *Grants) Get(_ context.Context, id string) (*domain.Grant, error) {
	if _, err := ids.ObjectID(id); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.data[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	return &g, nil
}

func (s *Grants) ListForUser(_ context.Context, userID string) ([]*domain.Grant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*domain.Grant{}
	for _, g := range s.data {
		if g.Owner.UserID == userID {
			out = append(out, &g)
		}
	}
	slices.SortFunc(out, func(a, b *domain.Grant) int {
		return cmp.Or(b.LastUsedAt.Compare(a.LastUsedAt), cmp.Compare(b.ID, a.ID))
	})
	return out, nil
}

func (s *Grants) Rotate(_ context.Context, r application.Rotation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data[r.GrantID]
	if !ok || g.RefreshHash != r.RefreshHash {
		return apperrors.ErrNotFound
	}
	g.AccessHash, g.AccessExpiresAt = r.AccessHash, r.AccessExpiresAt
	g.RefreshHash, g.RefreshExpiresAt = r.NewRefreshHash, r.RefreshExpiresAt
	g.LastUsedAt = r.UsedAt
	s.data[r.GrantID] = g
	return nil
}

func (s *Grants) Delete(_ context.Context, userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data[id]
	if !ok || g.Owner.UserID != userID {
		return apperrors.ErrNotFound
	}
	delete(s.data, id)
	return nil
}

func (s *Grants) Revoke(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}

func (s *Grants) DeleteForUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, g := range s.data {
		if g.Owner.UserID == userID {
			delete(s.data, id)
		}
	}
	return nil
}
