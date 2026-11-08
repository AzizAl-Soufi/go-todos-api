// Package application holds the teams module's ports. The use cases live in
// its two subpackages, following CQRS:
//
//	command/  writes: create, rename and delete teams; invite, accept,
//	          decline and revoke; change roles, remove members, leave,
//	          transfer ownership; remove a deleted account
//	query/    reads: the caller's teams, a team with its members, pending
//	          invitations, the caller's memberships (for access.Resolver)
package application

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
)

// Repositories return apperrors.ErrNotFound for missing records and
// apperrors.ErrInvalidID for IDs in the wrong format. Calls made with a
// context from tx.Runner.WithinTx must join that transaction.

// ---- write side (commands) ----

type TeamRepository interface {
	TeamReader
	// Create stores t and assigns t.ID.
	Create(ctx context.Context, t *domain.Team) error
	Update(ctx context.Context, t *domain.Team) error
	Delete(ctx context.Context, id string) error
}

type MemberRepository interface {
	MemberReader
	// Add stores a membership; an existing one is apperrors.ErrDuplicate.
	Add(ctx context.Context, m *domain.Member) error
	SetRole(ctx context.Context, teamID, userID string, role access.Role) error
	Remove(ctx context.Context, teamID, userID string) error
	DeleteByTeam(ctx context.Context, teamID string) error
}

type InvitationRepository interface {
	InvitationReader
	// Save stores inv and assigns inv.ID, replacing a pending invitation for
	// the same team and email.
	Save(ctx context.Context, inv *domain.Invitation) error
	FindByTokenHash(ctx context.Context, tokenHash string) (*domain.Invitation, error)
	// Resend gives an invitation a new token, expiry and send time, keeping
	// its ID; only if it was last sent at or before r.SentBefore, so two
	// resends at once can't both pass the cooldown. ErrNotFound when the
	// invitation is gone or was sent later.
	Resend(ctx context.Context, r InvitationResend) error
	Delete(ctx context.Context, teamID, id string) error
	DeleteByTeam(ctx context.Context, teamID string) error
	// DeleteExpired deletes up to limit invitations of any team that expired
	// before `before`, and returns how many it deleted.
	DeleteExpired(ctx context.Context, before time.Time, limit int) (int64, error)
}

// InvitationResend is a new link for an invitation (InvitationRepository.Resend).
type InvitationResend struct {
	TeamID, ID string
	TokenHash  string
	SentAt     time.Time
	ExpiresAt  time.Time
	SentBefore time.Time // the cooldown: last sent at or before this
}

// TeamData is how team deletion reaches the team's projects, collections and
// todos (the todos module).
type TeamData interface {
	DeleteTeamData(ctx context.Context, teamID string) error
}

// AuditEntry is the teams module's view of a team audit event.
type AuditEntry struct {
	TeamID, ActorID      string
	Action               string // e.g. "member.role_changed"
	TargetType, TargetID string
	Details              map[string]string
}

// AuditLog is how commands reach the audit module.
type AuditLog interface {
	Record(ctx context.Context, e AuditEntry) error
	DeleteTeamLog(ctx context.Context, teamID string) error
}

type Mailer interface {
	SendInvitation(ctx context.Context, to, inviterName, teamName string, role access.Role, link string) error
}

// ---- read side (queries) ----

type TeamReader interface {
	Get(ctx context.Context, id string) (*domain.Team, error)
}

type MemberReader interface {
	Get(ctx context.Context, teamID, userID string) (*domain.Member, error)
	// ListByTeam returns the team's members, earliest joined first.
	ListByTeam(ctx context.Context, teamID string) ([]*domain.Member, error)
	// ListByUser returns the user's memberships, earliest joined first.
	ListByUser(ctx context.Context, userID string) ([]*domain.Member, error)
}

type InvitationReader interface {
	// Get returns a pending invitation of the team.
	Get(ctx context.Context, teamID, id string) (*domain.Invitation, error)
	// ListByTeam returns the team's pending invitations, oldest first.
	ListByTeam(ctx context.Context, teamID string) ([]*domain.Invitation, error)
}
