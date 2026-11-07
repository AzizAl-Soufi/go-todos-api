// Package memstore holds the in-memory teams, members and invitations
// repositories (local development and tests). Values crossing the lock
// boundary are copies.
package memstore

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ---- teams ----

type Teams struct {
	mu    sync.RWMutex
	teams map[string]domain.Team
}

var _ application.TeamRepository = (*Teams)(nil)

func NewTeams() *Teams { return &Teams{teams: map[string]domain.Team{}} }

func (s *Teams) Create(_ context.Context, t *domain.Team) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.ID = bson.NewObjectID().Hex()
	s.teams[t.ID] = *t
	return nil
}

func (s *Teams) Get(_ context.Context, id string) (*domain.Team, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.teams[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	return &t, nil
}

func (s *Teams) Update(_ context.Context, t *domain.Team) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.teams[t.ID]; !ok {
		return apperrors.ErrNotFound
	}
	s.teams[t.ID] = *t
	return nil
}

func (s *Teams) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.teams[id]; !ok {
		return apperrors.ErrNotFound
	}
	delete(s.teams, id)
	return nil
}

// ---- members ----

type memberKey struct{ team, user string }

type Members struct {
	mu      sync.RWMutex
	members map[memberKey]domain.Member
}

var _ application.MemberRepository = (*Members)(nil)

func NewMembers() *Members { return &Members{members: map[memberKey]domain.Member{}} }

func (s *Members) Add(_ context.Context, m *domain.Member) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memberKey{m.TeamID, m.UserID}
	if _, ok := s.members[k]; ok {
		return apperrors.ErrDuplicate
	}
	s.members[k] = *m
	return nil
}

func (s *Members) Get(_ context.Context, teamID, userID string) (*domain.Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.members[memberKey{teamID, userID}]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	return &m, nil
}

func (s *Members) SetRole(_ context.Context, teamID, userID string, role access.Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memberKey{teamID, userID}
	m, ok := s.members[k]
	if !ok {
		return apperrors.ErrNotFound
	}
	m.Role = role
	s.members[k] = m
	return nil
}

func (s *Members) Remove(_ context.Context, teamID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memberKey{teamID, userID}
	if _, ok := s.members[k]; !ok {
		return apperrors.ErrNotFound
	}
	delete(s.members, k)
	return nil
}

// list returns the matching members, earliest joined first.
func (s *Members) list(match func(domain.Member) bool) []*domain.Member {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*domain.Member{}
	for _, m := range s.members {
		if match(m) {
			out = append(out, &m)
		}
	}
	slices.SortFunc(out, func(a, b *domain.Member) int {
		return cmp.Or(a.JoinedAt.Compare(b.JoinedAt), cmp.Compare(a.TeamID, b.TeamID), cmp.Compare(a.UserID, b.UserID))
	})
	return out
}

func (s *Members) ListByTeam(_ context.Context, teamID string) ([]*domain.Member, error) {
	return s.list(func(m domain.Member) bool { return m.TeamID == teamID }), nil
}

func (s *Members) ListByUser(_ context.Context, userID string) ([]*domain.Member, error) {
	return s.list(func(m domain.Member) bool { return m.UserID == userID }), nil
}

func (s *Members) DeleteByTeam(_ context.Context, teamID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.members {
		if k.team == teamID {
			delete(s.members, k)
		}
	}
	return nil
}

// ---- invitations ----

type Invitations struct {
	mu   sync.RWMutex
	data map[string]domain.Invitation
}

var _ application.InvitationRepository = (*Invitations)(nil)

func NewInvitations() *Invitations { return &Invitations{data: map[string]domain.Invitation{}} }

func (s *Invitations) Save(_ context.Context, inv *domain.Invitation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.data {
		if old.TeamID == inv.TeamID && old.Email == inv.Email {
			delete(s.data, id)
		}
	}
	inv.ID = bson.NewObjectID().Hex()
	s.data[inv.ID] = *inv
	return nil
}

func (s *Invitations) find(match func(domain.Invitation) bool) (*domain.Invitation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, inv := range s.data {
		if match(inv) {
			return &inv, nil
		}
	}
	return nil, apperrors.ErrNotFound
}

func (s *Invitations) Get(_ context.Context, teamID, id string) (*domain.Invitation, error) {
	return s.find(func(i domain.Invitation) bool { return i.ID == id && i.TeamID == teamID })
}

func (s *Invitations) FindByTokenHash(_ context.Context, tokenHash string) (*domain.Invitation, error) {
	return s.find(func(i domain.Invitation) bool { return i.TokenHash == tokenHash })
}

func (s *Invitations) Delete(_ context.Context, teamID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.data[id]
	if !ok || inv.TeamID != teamID {
		return apperrors.ErrNotFound
	}
	delete(s.data, id)
	return nil
}

func (s *Invitations) ListByTeam(_ context.Context, teamID string) ([]*domain.Invitation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*domain.Invitation{}
	for _, inv := range s.data {
		if inv.TeamID == teamID {
			out = append(out, &inv)
		}
	}
	slices.SortFunc(out, func(a, b *domain.Invitation) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

func (s *Invitations) DeleteByTeam(_ context.Context, teamID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, inv := range s.data {
		if inv.TeamID == teamID {
			delete(s.data, id)
		}
	}
	return nil
}
