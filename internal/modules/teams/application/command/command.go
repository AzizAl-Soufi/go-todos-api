// Package command holds the teams module's write use cases. Roles are always
// checked against the stored membership (not the caller's cached Actor), and
// every change is written to the team audit log in the same unit of work.
package command

import (
	"context"
	"log/slog"
	"net/url"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

type Config struct {
	InvitationTTL time.Duration
	// AcceptURL is the link base in invitation emails; the token is appended
	// as ?token=... It should open a client page that signs the user in and
	// calls POST /invitations/accept.
	AcceptURL string
}

func DefaultConfig(baseURL string) Config {
	return Config{InvitationTTL: 7 * 24 * time.Hour, AcceptURL: baseURL + "/invitations/accept"}
}

type Deps struct {
	Teams       application.TeamRepository
	Members     application.MemberRepository
	Invitations application.InvitationRepository
	TeamData    application.TeamData
	Audit       application.AuditLog
	Mailer      application.Mailer
	Tx          tx.Runner
	Config      Config
	Now         func() time.Time // nil means time.Now
}

type Handlers struct {
	CreateTeam        CreateTeamHandler
	RenameTeam        RenameTeamHandler
	DeleteTeam        DeleteTeamHandler
	Invite            InviteHandler
	RevokeInvitation  RevokeInvitationHandler
	AcceptInvitation  AcceptInvitationHandler
	DeclineInvitation DeclineInvitationHandler
	ChangeRole        ChangeRoleHandler
	RemoveMember      RemoveMemberHandler
	TransferOwnership TransferOwnershipHandler
	RemoveUser        RemoveUserHandler
}

func New(d Deps) Handlers {
	if d.Tx == nil {
		d.Tx = tx.None{}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	b := base{d}
	return Handlers{
		CreateTeam: CreateTeamHandler{b}, RenameTeam: RenameTeamHandler{b}, DeleteTeam: DeleteTeamHandler{b},
		Invite: InviteHandler{b}, RevokeInvitation: RevokeInvitationHandler{b},
		AcceptInvitation: AcceptInvitationHandler{b}, DeclineInvitation: DeclineInvitationHandler{b},
		ChangeRole: ChangeRoleHandler{b}, RemoveMember: RemoveMemberHandler{b},
		TransferOwnership: TransferOwnershipHandler{b}, RemoveUser: RemoveUserHandler{b},
	}
}

type base struct{ Deps }

func (b base) now() time.Time { return domain.Timestamp(b.Now()) }

// membership returns the user's stored membership; non-members (and
// malformed team IDs) get access.ErrTeamNotFound so team IDs don't leak.
func (b base) membership(ctx context.Context, teamID, userID string) (*domain.Member, error) {
	m, err := b.Members.Get(ctx, teamID, userID)
	return m, apperrors.UnresolvedAs(err, access.ErrTeamNotFound)
}

// manager returns the actor's membership if their role passes allowed.
func (b base) manager(ctx context.Context, teamID string, a access.Actor, allowed func(access.Role) bool) (*domain.Member, error) {
	m, err := b.membership(ctx, teamID, a.UserID)
	if err != nil {
		return nil, err
	}
	if !allowed(m.Role) {
		return nil, access.ErrInsufficientRole
	}
	return m, nil
}

// target returns another member of the team.
func (b base) target(ctx context.Context, teamID, userID string) (*domain.Member, error) {
	m, err := b.Members.Get(ctx, teamID, userID)
	return m, apperrors.UnresolvedAs(err, domain.ErrMemberNotFound)
}

func (b base) record(ctx context.Context, a access.Actor, teamID, action, targetType, targetID string, details map[string]string) error {
	return b.Audit.Record(ctx, application.AuditEntry{
		TeamID: teamID, ActorID: a.UserID, Action: action, TargetType: targetType, TargetID: targetID, Details: details,
	})
}

// deleteTeam removes a team with everything that belongs to it.
func (b base) deleteTeam(ctx context.Context, teamID string) error {
	for _, step := range []func(context.Context, string) error{
		b.TeamData.DeleteTeamData,
		b.Invitations.DeleteByTeam,
		b.Members.DeleteByTeam,
		b.Audit.DeleteTeamLog,
		b.Teams.Delete,
	} {
		if err := step(ctx, teamID); err != nil {
			return err
		}
	}
	return nil
}

func inviteLink(base, token string) string { return base + "?token=" + url.QueryEscape(token) }

// sendInvitation never fails the command: an admin can invite again.
func (b base) sendInvitation(ctx context.Context, inv *domain.Invitation, inviter, team, link string) {
	if err := b.Mailer.SendInvitation(ctx, inv.Email, inviter, team, inv.Role, link); err != nil {
		slog.Error("failed to send invitation email", "team_id", inv.TeamID, "invitation_id", inv.ID, "error", err)
	}
}

// ---- teams ----

// CreateTeam creates a team owned by the actor.
type CreateTeam struct {
	Actor access.Actor
	Name  string
}

type CreateTeamHandler struct{ base }

func (h CreateTeamHandler) Handle(ctx context.Context, cmd CreateTeam) (id string, err error) {
	team, err := domain.NewTeam(cmd.Name, h.now())
	if err != nil {
		return "", err
	}
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Teams.Create(ctx, team); err != nil {
			return err
		}
		owner := &domain.Member{
			TeamID: team.ID, UserID: cmd.Actor.UserID, Name: cmd.Actor.Name, Email: cmd.Actor.Email,
			Role: access.Owner, JoinedAt: team.CreatedAt,
		}
		if err := h.Members.Add(ctx, owner); err != nil {
			return err
		}
		return h.record(ctx, cmd.Actor, team.ID, "team.created", "team", team.ID, map[string]string{"name": team.Name})
	})
	return team.ID, err
}

// RenameTeam renames a team; owners and admins only.
type RenameTeam struct {
	Actor  access.Actor
	TeamID string
	Name   string
}

type RenameTeamHandler struct{ base }

func (h RenameTeamHandler) Handle(ctx context.Context, cmd RenameTeam) error {
	if _, err := h.manager(ctx, cmd.TeamID, cmd.Actor, access.Role.CanManage); err != nil {
		return err
	}
	team, err := h.Teams.Get(ctx, cmd.TeamID)
	if err != nil {
		return apperrors.NotFoundAs(err, access.ErrTeamNotFound)
	}
	from := team.Name
	if err := team.Rename(cmd.Name, h.now()); err != nil {
		return err
	}
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Teams.Update(ctx, team); err != nil {
			return err
		}
		return h.record(ctx, cmd.Actor, team.ID, "team.renamed", "team", team.ID, map[string]string{"from": from, "to": team.Name})
	})
}

// DeleteTeam deletes a team with its members, invitations, audit log and all
// its projects, collections and todos; the owner only.
type DeleteTeam struct {
	Actor  access.Actor
	TeamID string
}

type DeleteTeamHandler struct{ base }

func (h DeleteTeamHandler) Handle(ctx context.Context, cmd DeleteTeam) error {
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := h.manager(ctx, cmd.TeamID, cmd.Actor, func(r access.Role) bool { return r == access.Owner }); err != nil {
			return err
		}
		return h.deleteTeam(ctx, cmd.TeamID)
	})
}

// RemoveUser takes a deleted account out of every team. Teams the user owns
// alone are deleted; owning a team that has other members blocks the account
// deletion (ErrOwnsTeamsWithMembers). Called with a transactional context it
// joins that transaction.
type RemoveUser struct {
	UserID string
}

type RemoveUserHandler struct{ base }

func (h RemoveUserHandler) Handle(ctx context.Context, cmd RemoveUser) error {
	memberships, err := h.Members.ListByUser(ctx, cmd.UserID)
	if err != nil {
		return err
	}
	actor := access.Personal(cmd.UserID)
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		for _, m := range memberships {
			if err := h.leave(ctx, actor, m); err != nil {
				return err
			}
		}
		return nil
	})
}

func (h RemoveUserHandler) leave(ctx context.Context, actor access.Actor, m *domain.Member) error {
	if m.Role != access.Owner {
		if err := h.Members.Remove(ctx, m.TeamID, m.UserID); err != nil {
			return err
		}
		return h.record(ctx, actor, m.TeamID, "member.left", "member", m.UserID, map[string]string{"reason": "account_deleted"})
	}
	members, err := h.Members.ListByTeam(ctx, m.TeamID)
	if err != nil {
		return err
	}
	if len(members) > 1 {
		return domain.ErrOwnsTeamsWithMembers
	}
	return h.deleteTeam(ctx, m.TeamID)
}
