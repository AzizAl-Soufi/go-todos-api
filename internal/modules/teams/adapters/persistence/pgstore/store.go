// Package pgstore holds the PostgreSQL teams, members and invitations
// repositories. Each call runs on the transaction carried by ctx when there
// is one, otherwise on the pool.
package pgstore

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/persistence/pgstore/sqlcgen"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
	"github.com/jackc/pgx/v5/pgxpool"
)

type base struct{ pool *pgxpool.Pool }

func (b base) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Executor(ctx, b.pool))
}

// ---- teams ----

type Teams struct{ base }

var _ application.TeamRepository = (*Teams)(nil)

func NewTeams(pool *pgxpool.Pool) *Teams { return &Teams{base{pool}} }

func teamToDomain(t sqlcgen.Team) *domain.Team {
	return &domain.Team{ID: t.ID.String(), Name: t.Name, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
}

func (s *Teams) Create(ctx context.Context, t *domain.Team) error {
	row, err := s.q(ctx).CreateTeam(ctx, sqlcgen.CreateTeamParams{Name: t.Name, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt})
	if err != nil {
		return postgres.MapErr("create team", err)
	}
	*t = *teamToDomain(row)
	return nil
}

func (s *Teams) Get(ctx context.Context, id string) (*domain.Team, error) {
	u, err := ids.UUID(id)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetTeam(ctx, u)
	if err != nil {
		return nil, postgres.MapErr("get team", err)
	}
	return teamToDomain(row), nil
}

func (s *Teams) Update(ctx context.Context, t *domain.Team) error {
	u, err := ids.UUID(t.ID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).UpdateTeam(ctx, sqlcgen.UpdateTeamParams{ID: u, Name: t.Name, UpdatedAt: t.UpdatedAt})
	return postgres.RequireAffected("update team", rows, err)
}

func (s *Teams) Delete(ctx context.Context, id string) error {
	u, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteTeam(ctx, u)
	return postgres.RequireAffected("delete team", rows, err)
}

// ---- members ----

type Members struct{ base }

var _ application.MemberRepository = (*Members)(nil)

func NewMembers(pool *pgxpool.Pool) *Members { return &Members{base{pool}} }

func memberToDomain(m sqlcgen.TeamMember) *domain.Member {
	return &domain.Member{
		TeamID: m.TeamID.String(), UserID: m.UserID.String(), Name: m.Name, Email: m.Email,
		Role: access.Role(m.Role), JoinedAt: m.JoinedAt,
	}
}

func membersToDomain(rows []sqlcgen.TeamMember, err error) ([]*domain.Member, error) {
	if err != nil {
		return nil, postgres.MapErr("list members", err)
	}
	out := make([]*domain.Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, memberToDomain(r))
	}
	return out, nil
}

func (s *Members) Add(ctx context.Context, m *domain.Member) error {
	teamID, userID, err := ids.UUIDPair(m.TeamID, m.UserID)
	if err != nil {
		return err
	}
	err = s.q(ctx).AddMember(ctx, sqlcgen.AddMemberParams{
		TeamID: teamID, UserID: userID, Name: m.Name, Email: m.Email, Role: string(m.Role), JoinedAt: m.JoinedAt,
	})
	if postgres.IsUniqueViolation(err) {
		return apperrors.ErrDuplicate
	}
	return postgres.MapErr("add member", err)
}

func (s *Members) Get(ctx context.Context, teamID, userID string) (*domain.Member, error) {
	team, user, err := ids.UUIDPair(teamID, userID)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetMember(ctx, sqlcgen.GetMemberParams{TeamID: team, UserID: user})
	if err != nil {
		return nil, postgres.MapErr("get member", err)
	}
	return memberToDomain(row), nil
}

func (s *Members) SetRole(ctx context.Context, teamID, userID string, role access.Role) error {
	team, user, err := ids.UUIDPair(teamID, userID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).SetMemberRole(ctx, sqlcgen.SetMemberRoleParams{TeamID: team, UserID: user, Role: string(role)})
	return postgres.RequireAffected("set member role", rows, err)
}

func (s *Members) Remove(ctx context.Context, teamID, userID string) error {
	team, user, err := ids.UUIDPair(teamID, userID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).RemoveMember(ctx, sqlcgen.RemoveMemberParams{TeamID: team, UserID: user})
	return postgres.RequireAffected("remove member", rows, err)
}

func (s *Members) ListByTeam(ctx context.Context, teamID string) ([]*domain.Member, error) {
	u, err := ids.UUID(teamID)
	if err != nil {
		return nil, err
	}
	return membersToDomain(s.q(ctx).ListMembersByTeam(ctx, u))
}

func (s *Members) ListByUser(ctx context.Context, userID string) ([]*domain.Member, error) {
	u, err := ids.UUID(userID)
	if err != nil {
		return nil, err
	}
	return membersToDomain(s.q(ctx).ListMembersByUser(ctx, u))
}

func (s *Members) DeleteByTeam(ctx context.Context, teamID string) error {
	u, err := ids.UUID(teamID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete members by team", s.q(ctx).DeleteMembersByTeam(ctx, u))
}

// ---- invitations ----

type Invitations struct{ base }

var _ application.InvitationRepository = (*Invitations)(nil)

func NewInvitations(pool *pgxpool.Pool) *Invitations { return &Invitations{base{pool}} }

func invitationToDomain(i sqlcgen.TeamInvitation) *domain.Invitation {
	return &domain.Invitation{
		ID: i.ID.String(), TeamID: i.TeamID.String(), Email: i.Email, Role: access.Role(i.Role),
		InvitedBy: i.InvitedBy.String(), TokenHash: i.TokenHash, ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt,
		SentAt: i.SentAt,
	}
}

func (s *Invitations) Save(ctx context.Context, inv *domain.Invitation) error {
	teamID, invitedBy, err := ids.UUIDPair(inv.TeamID, inv.InvitedBy)
	if err != nil {
		return err
	}
	id, err := s.q(ctx).SaveInvitation(ctx, sqlcgen.SaveInvitationParams{
		TeamID: teamID, Email: inv.Email, Role: string(inv.Role), InvitedBy: invitedBy,
		TokenHash: inv.TokenHash, ExpiresAt: inv.ExpiresAt, CreatedAt: inv.CreatedAt, SentAt: inv.SentAt,
	})
	if err != nil {
		return postgres.MapErr("save invitation", err)
	}
	inv.ID = id.String()
	return nil
}

func (s *Invitations) Get(ctx context.Context, teamID, id string) (*domain.Invitation, error) {
	invID, team, err := ids.UUIDPair(id, teamID)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetInvitation(ctx, sqlcgen.GetInvitationParams{TeamID: team, ID: invID})
	if err != nil {
		return nil, postgres.MapErr("get invitation", err)
	}
	return invitationToDomain(row), nil
}

func (s *Invitations) FindByTokenHash(ctx context.Context, tokenHash string) (*domain.Invitation, error) {
	row, err := s.q(ctx).FindInvitationByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, postgres.MapErr("find invitation", err)
	}
	return invitationToDomain(row), nil
}

func (s *Invitations) Resend(ctx context.Context, r application.InvitationResend) error {
	invID, team, err := ids.UUIDPair(r.ID, r.TeamID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).ResendInvitation(ctx, sqlcgen.ResendInvitationParams{
		TeamID:     team,
		ID:         invID,
		TokenHash:  r.TokenHash,
		ExpiresAt:  r.ExpiresAt,
		SentAt:     r.SentAt,
		SentBefore: r.SentBefore,
	})
	return postgres.RequireAffected("resend invitation", rows, err)
}

func (s *Invitations) Delete(ctx context.Context, teamID, id string) error {
	invID, team, err := ids.UUIDPair(id, teamID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteInvitation(ctx, sqlcgen.DeleteInvitationParams{TeamID: team, ID: invID})
	return postgres.RequireAffected("delete invitation", rows, err)
}

func (s *Invitations) ListByTeam(ctx context.Context, teamID string) ([]*domain.Invitation, error) {
	u, err := ids.UUID(teamID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).ListInvitationsByTeam(ctx, u)
	if err != nil {
		return nil, postgres.MapErr("list invitations", err)
	}
	out := make([]*domain.Invitation, 0, len(rows))
	for _, r := range rows {
		out = append(out, invitationToDomain(r))
	}
	return out, nil
}

func (s *Invitations) DeleteByTeam(ctx context.Context, teamID string) error {
	u, err := ids.UUID(teamID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete invitations by team", s.q(ctx).DeleteInvitationsByTeam(ctx, u))
}
