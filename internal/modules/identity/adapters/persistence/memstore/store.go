// Package memstore holds in-memory identity repositories (local dev, tests).
// Values are copied on the way in and out, so callers never share stored state.
package memstore

import (
	"bytes"
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	inmem "github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/memory"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func newID() string { return bson.NewObjectID().Hex() }

// ---- users ----

type Users struct {
	inmem.MemoryClient[string, domain.User]
}

var _ application.UserRepository = (*Users)(nil)

func NewUsers() *Users {
	return &Users{MemoryClient: inmem.MemoryClient[string, domain.User]{Data: map[string]*domain.User{}}}
}

func (s *Users) Create(_ context.Context, u *domain.User) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	for _, existing := range s.Data {
		if existing.Email == u.Email {
			return domain.ErrEmailTaken
		}
		if u.FirebaseUID != "" && existing.FirebaseUID == u.FirebaseUID {
			return domain.ErrFirebaseUIDTaken
		}
	}
	u.ID = newID()
	s.Data[u.ID] = inmem.Clone(u)
	return nil
}

func (s *Users) FindByID(_ context.Context, id string) (*domain.User, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	u, ok := s.Data[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	return inmem.Clone(u), nil
}

func (s *Users) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	for _, u := range s.Data {
		if u.Email == email {
			return inmem.Clone(u), nil
		}
	}
	return nil, apperrors.ErrNotFound
}

func (s *Users) FindByFirebaseUID(_ context.Context, firebaseUID string) (*domain.User, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	for _, u := range s.Data {
		if firebaseUID != "" && u.FirebaseUID == firebaseUID {
			return inmem.Clone(u), nil
		}
	}
	return nil, apperrors.ErrNotFound
}

func (s *Users) LinkFirebaseUID(_ context.Context, id, firebaseUID string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	u, ok := s.Data[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	if u.FirebaseUID != "" {
		return domain.ErrFirebaseUIDTaken
	}
	for _, other := range s.Data {
		if other.FirebaseUID == firebaseUID {
			return domain.ErrFirebaseUIDTaken
		}
	}
	u.FirebaseUID = firebaseUID
	return nil
}

func (s *Users) update(id string, fn func(u *domain.User)) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	u, ok := s.Data[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	fn(u)
	return nil
}

func (s *Users) MarkEmailVerified(_ context.Context, id string, at time.Time) error {
	return s.update(id, func(u *domain.User) {
		if u.EmailVerifiedAt == nil {
			u.EmailVerifiedAt = &at
		}
	})
}

func (s *Users) RevokeSessions(_ context.Context, id string, at time.Time) error {
	return s.update(id, func(u *domain.User) { u.SessionsValidAfter = &at })
}

func (s *Users) Delete(_ context.Context, id string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	if _, ok := s.Data[id]; !ok {
		return apperrors.ErrNotFound
	}
	delete(s.Data, id)
	return nil
}

// ---- passkeys ----

type Passkeys struct {
	inmem.MemoryClient[string, domain.Passkey]
}

var _ application.PasskeyRepository = (*Passkeys)(nil)

func NewPasskeys() *Passkeys {
	return &Passkeys{MemoryClient: inmem.MemoryClient[string, domain.Passkey]{Data: map[string]*domain.Passkey{}}}
}

// clonePasskey copies the byte slices too: Clone is shallow.
func clonePasskey(p *domain.Passkey) *domain.Passkey {
	c := inmem.Clone(p)
	c.CredentialID = bytes.Clone(p.CredentialID)
	c.Credential = bytes.Clone(p.Credential)
	if p.LastUsedAt != nil {
		at := *p.LastUsedAt
		c.LastUsedAt = &at
	}
	return c
}

func (s *Passkeys) Create(_ context.Context, p *domain.Passkey) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	for _, existing := range s.Data {
		if bytes.Equal(existing.CredentialID, p.CredentialID) {
			return domain.ErrPasskeyExists
		}
	}
	p.ID = newID()
	s.Data[p.ID] = clonePasskey(p)
	return nil
}

func (s *Passkeys) ListForUser(_ context.Context, userID string) ([]*domain.Passkey, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	out := []*domain.Passkey{}
	for _, p := range s.Data {
		if p.UserID == userID {
			out = append(out, clonePasskey(p))
		}
	}
	slices.SortFunc(out, func(a, b *domain.Passkey) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

func (s *Passkeys) RecordUse(_ context.Context, id string, credential []byte, at time.Time) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	p, ok := s.Data[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	p.Credential = bytes.Clone(credential)
	p.LastUsedAt = &at
	return nil
}

func (s *Passkeys) Delete(_ context.Context, userID, id string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	if p, ok := s.Data[id]; !ok || p.UserID != userID {
		return apperrors.ErrNotFound
	}
	delete(s.Data, id)
	return nil
}

func (s *Passkeys) DeleteAllForUser(_ context.Context, userID string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	for id, p := range s.Data {
		if p.UserID == userID {
			delete(s.Data, id)
		}
	}
	return nil
}

// ---- passkey challenges (keyed by token hash) ----

type PasskeyChallenges struct {
	inmem.MemoryClient[string, domain.PasskeyChallenge]
}

var _ application.PasskeyChallengeRepository = (*PasskeyChallenges)(nil)

func NewPasskeyChallenges() *PasskeyChallenges {
	return &PasskeyChallenges{MemoryClient: inmem.MemoryClient[string, domain.PasskeyChallenge]{Data: map[string]*domain.PasskeyChallenge{}}}
}

func (s *PasskeyChallenges) Save(_ context.Context, c *domain.PasskeyChallenge) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	for hash, existing := range s.Data {
		if existing.ExpiresAt.Before(c.CreatedAt) {
			delete(s.Data, hash)
		}
	}
	stored := inmem.Clone(c)
	stored.State = bytes.Clone(c.State)
	s.Data[c.TokenHash] = stored
	return nil
}

func (s *PasskeyChallenges) Consume(_ context.Context, tokenHash string) (*domain.PasskeyChallenge, error) {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	c, ok := s.Data[tokenHash]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	delete(s.Data, tokenHash)
	return c, nil // removed from the map, so returning it shares nothing
}

func (s *PasskeyChallenges) DeleteForUser(_ context.Context, userID string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	for hash, c := range s.Data {
		if c.UserID == userID {
			delete(s.Data, hash)
		}
	}
	return nil
}
