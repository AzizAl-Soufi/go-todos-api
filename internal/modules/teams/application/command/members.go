package command

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/opaque"
)

// ---- invitations ----

// Invite emails an invitation to join the team with a role below the
// actor's. Inviting the same email again replaces the pending invitation (and
// its link), but like a resend only once domain.InvitationResendCooldown has
// passed since that invitation was last sent: otherwise the same email
// address could be sent mail without limit.
type Invite struct {
	Actor  access.Actor
	TeamID string
	Email  string
	Role   access.Role
}

type InviteHandler struct{ base }

func (h InviteHandler) Handle(ctx context.Context, cmd Invite) (invitationID string, err error) {
	inviter, err := h.membership(ctx, cmd.TeamID, cmd.Actor.UserID)
	if err != nil {
		return "", err
	}
	if err := domain.CanInvite(inviter.Role, cmd.Role); err != nil {
		return "", err
	}
	email, err := domain.NormalizeEmail(cmd.Email)
	if err != nil {
		return "", err
	}
	team, err := h.Teams.Get(ctx, cmd.TeamID)
	if err != nil {
		return "", apperrors.NotFoundAs(err, access.ErrTeamNotFound)
	}
	members, err := h.Members.ListByTeam(ctx, cmd.TeamID)
	if err != nil {
		return "", err
	}
	for _, m := range members {
		if strings.EqualFold(m.Email, email) {
			return "", domain.ErrAlreadyMember
		}
	}
	now := h.now()
	if err := h.checkReinvite(ctx, cmd.TeamID, email, now); err != nil {
		return "", err
	}

	token, hash, err := opaque.New()
	if err != nil {
		return "", err
	}
	inv := &domain.Invitation{
		TeamID: cmd.TeamID, Email: email, Role: cmd.Role, InvitedBy: cmd.Actor.UserID,
		TokenHash: hash, ExpiresAt: now.Add(h.Config.InvitationTTL), CreatedAt: now, SentAt: now,
	}
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Invitations.Save(ctx, inv); err != nil {
			return err
		}
		return h.record(ctx, cmd.Actor, cmd.TeamID, "invitation.created", "invitation", inv.ID,
			map[string]string{"email": email, "role": string(cmd.Role)})
	})
	if err != nil {
		return "", err
	}
	h.sendInvitation(ctx, inv, inviter.Name, team.Name, inviteLink(h.Config.AcceptURL, token))
	return inv.ID, nil
}

// checkReinvite applies the resend cooldown to a pending invitation for the
// same email. A team has few pending invitations, so listing them is cheap.
// Two invitations at the same instant can both pass; the cooldown is a
// guard against flooding, not an invariant.
func (h InviteHandler) checkReinvite(ctx context.Context, teamID, email string, now time.Time) error {
	pending, err := h.Invitations.ListByTeam(ctx, teamID)
	if err != nil {
		return err
	}
	for _, inv := range pending {
		if inv.Email == email {
			return inv.CheckResend(now)
		}
	}
	return nil
}

// ResendInvitation emails an invitation again with a new link: the old link
// stops working, and the invitation (expired or not) is valid for
// InvitationTTL from now. It keeps its ID, email and role. Whoever may
// invite with that role may resend, at most once per
// domain.InvitationResendCooldown; sooner fails with ErrResendTooSoon,
// retryable from the time it becomes available.
type ResendInvitation struct {
	Actor      access.Actor
	TeamID, ID string
}

type ResendInvitationHandler struct{ base }

func (h ResendInvitationHandler) Handle(ctx context.Context, cmd ResendInvitation) error {
	inviter, err := h.membership(ctx, cmd.TeamID, cmd.Actor.UserID)
	if err != nil {
		return err
	}
	if !inviter.Role.CanManage() {
		return access.ErrInsufficientRole
	}
	inv, err := h.Invitations.Get(ctx, cmd.TeamID, cmd.ID)
	if err != nil {
		return apperrors.UnresolvedAs(err, domain.ErrInvitationNotFound)
	}
	if err := domain.CanInvite(inviter.Role, inv.Role); err != nil {
		return err
	}
	now := h.now()
	if err := inv.CheckResend(now); err != nil {
		return err
	}
	team, err := h.Teams.Get(ctx, cmd.TeamID)
	if err != nil {
		return apperrors.NotFoundAs(err, access.ErrTeamNotFound)
	}

	token, hash, err := opaque.New()
	if err != nil {
		return err
	}
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		err := h.Invitations.Resend(ctx, application.InvitationResend{
			TeamID:     cmd.TeamID,
			ID:         inv.ID,
			TokenHash:  hash,
			SentAt:     now,
			ExpiresAt:  now.Add(h.Config.InvitationTTL),
			SentBefore: now.Add(-domain.InvitationResendCooldown),
		})
		if errors.Is(err, apperrors.ErrNotFound) {
			return h.resendLost(ctx, cmd, now)
		}
		if err != nil {
			return err
		}
		return h.record(ctx, cmd.Actor, cmd.TeamID, "invitation.resent", "invitation", inv.ID,
			map[string]string{"email": inv.Email})
	})
	if err != nil {
		return err
	}
	h.sendInvitation(ctx, inv, inviter.Name, team.Name, inviteLink(h.Config.AcceptURL, token))
	return nil
}

// resendLost explains why the guarded update matched nothing: the
// invitation was revoked or accepted meanwhile, or another resend got in
// first (then it is too soon again).
func (h ResendInvitationHandler) resendLost(ctx context.Context, cmd ResendInvitation, now time.Time) error {
	inv, err := h.Invitations.Get(ctx, cmd.TeamID, cmd.ID)
	if err != nil {
		return apperrors.NotFoundAs(err, domain.ErrInvitationNotFound)
	}
	if err := inv.CheckResend(now); err != nil {
		return err
	}
	// The stored send time and ours disagree by a rounding step; a moment
	// later the guard will match.
	return apperrors.RetryAt(domain.ErrResendTooSoon, now, now.Add(time.Second))
}

// RevokeInvitation cancels a pending invitation; owners and admins only.
type RevokeInvitation struct {
	Actor      access.Actor
	TeamID, ID string
}

type RevokeInvitationHandler struct{ base }

func (h RevokeInvitationHandler) Handle(ctx context.Context, cmd RevokeInvitation) error {
	if _, err := h.manager(ctx, cmd.TeamID, cmd.Actor, access.Role.CanManage); err != nil {
		return err
	}
	inv, err := h.Invitations.Get(ctx, cmd.TeamID, cmd.ID)
	if err != nil {
		return apperrors.UnresolvedAs(err, domain.ErrInvitationNotFound)
	}
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Invitations.Delete(ctx, cmd.TeamID, inv.ID); err != nil {
			return apperrors.NotFoundAs(err, domain.ErrInvitationNotFound)
		}
		return h.record(ctx, cmd.Actor, cmd.TeamID, "invitation.revoked", "invitation", inv.ID, map[string]string{"email": inv.Email})
	})
}

// pending finds the invitation a token belongs to, for the signed-in actor:
// it must be unexpired and addressed to the actor's email.
func (b base) pending(ctx context.Context, a access.Actor, token string) (*domain.Invitation, error) {
	if token == "" {
		return nil, domain.ErrInvalidInvitation
	}
	inv, err := b.Invitations.FindByTokenHash(ctx, opaque.Hash(token))
	if err != nil {
		return nil, apperrors.NotFoundAs(err, domain.ErrInvalidInvitation)
	}
	if inv.Expired(b.now()) {
		return nil, domain.ErrInvalidInvitation
	}
	if !strings.EqualFold(inv.Email, a.Email) {
		return nil, domain.ErrInvitationForOther
	}
	return inv, nil
}

// AcceptInvitation adds the actor to the team with the invited role. The
// invitation must be addressed to the actor's email; it is used up.
type AcceptInvitation struct {
	Actor access.Actor
	Token string
}

type AcceptInvitationHandler struct{ base }

func (h AcceptInvitationHandler) Handle(ctx context.Context, cmd AcceptInvitation) (teamID string, err error) {
	inv, err := h.pending(ctx, cmd.Actor, cmd.Token)
	if err != nil {
		return "", err
	}
	if _, err := h.Members.Get(ctx, inv.TeamID, cmd.Actor.UserID); err == nil {
		// Already in: the invitation has nothing left to do.
		if err := h.Invitations.Delete(ctx, inv.TeamID, inv.ID); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			return "", err
		}
		return "", domain.ErrAlreadyMember
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return "", err
	}
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Invitations.Delete(ctx, inv.TeamID, inv.ID); err != nil {
			// A concurrent accept or decline used it first.
			return apperrors.NotFoundAs(err, domain.ErrInvalidInvitation)
		}
		m := &domain.Member{
			TeamID: inv.TeamID, UserID: cmd.Actor.UserID, Name: cmd.Actor.Name, Email: cmd.Actor.Email,
			Role: inv.Role, JoinedAt: h.now(),
		}
		if err := h.Members.Add(ctx, m); err != nil {
			if errors.Is(err, apperrors.ErrDuplicate) {
				return domain.ErrAlreadyMember
			}
			return err
		}
		return h.record(ctx, cmd.Actor, inv.TeamID, "member.joined", "member", cmd.Actor.UserID,
			map[string]string{"role": string(inv.Role), "invitedBy": inv.InvitedBy})
	})
	return inv.TeamID, err
}

// DeclineInvitation turns an invitation down; it is used up.
type DeclineInvitation struct {
	Actor access.Actor
	Token string
}

type DeclineInvitationHandler struct{ base }

func (h DeclineInvitationHandler) Handle(ctx context.Context, cmd DeclineInvitation) error {
	inv, err := h.pending(ctx, cmd.Actor, cmd.Token)
	if err != nil {
		return err
	}
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Invitations.Delete(ctx, inv.TeamID, inv.ID); err != nil {
			return apperrors.NotFoundAs(err, domain.ErrInvalidInvitation)
		}
		return h.record(ctx, cmd.Actor, inv.TeamID, "invitation.declined", "invitation", inv.ID, map[string]string{"email": inv.Email})
	})
}

// ---- members ----

// ChangeRole gives another member a new role (never owner: see
// TransferOwnership). The actor must outrank both the old and the new role.
type ChangeRole struct {
	Actor          access.Actor
	TeamID, UserID string
	Role           access.Role
}

type ChangeRoleHandler struct{ base }

func (h ChangeRoleHandler) Handle(ctx context.Context, cmd ChangeRole) error {
	actor, err := h.membership(ctx, cmd.TeamID, cmd.Actor.UserID)
	if err != nil {
		return err
	}
	if cmd.UserID == cmd.Actor.UserID {
		return domain.ErrCannotChangeOwnRole
	}
	target, err := h.target(ctx, cmd.TeamID, cmd.UserID)
	if err != nil {
		return err
	}
	if err := domain.CanChangeRole(actor.Role, target.Role, cmd.Role); err != nil || target.Role == cmd.Role {
		return err
	}
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Members.SetRole(ctx, cmd.TeamID, cmd.UserID, cmd.Role); err != nil {
			return apperrors.NotFoundAs(err, domain.ErrMemberNotFound)
		}
		return h.record(ctx, cmd.Actor, cmd.TeamID, "member.role_changed", "member", cmd.UserID,
			map[string]string{"from": string(target.Role), "to": string(cmd.Role)})
	})
}

// RemoveMember removes another member, or (UserID is the actor) leaves the
// team. The owner can't leave; managers only remove people below them.
type RemoveMember struct {
	Actor          access.Actor
	TeamID, UserID string
}

type RemoveMemberHandler struct{ base }

func (h RemoveMemberHandler) Handle(ctx context.Context, cmd RemoveMember) error {
	actor, err := h.membership(ctx, cmd.TeamID, cmd.Actor.UserID)
	if err != nil {
		return err
	}
	action, details := "member.left", map[string]string{}
	if cmd.UserID == cmd.Actor.UserID {
		if actor.Role == access.Owner {
			return domain.ErrOwnerCannotLeave
		}
	} else {
		target, err := h.target(ctx, cmd.TeamID, cmd.UserID)
		if err != nil {
			return err
		}
		if err := domain.CanRemove(actor.Role, target.Role); err != nil {
			return err
		}
		action, details = "member.removed", map[string]string{"role": string(target.Role), "email": target.Email}
	}
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Members.Remove(ctx, cmd.TeamID, cmd.UserID); err != nil {
			return apperrors.NotFoundAs(err, domain.ErrMemberNotFound)
		}
		return h.record(ctx, cmd.Actor, cmd.TeamID, action, "member", cmd.UserID, details)
	})
}

// TransferOwnership makes another member the owner; the previous owner
// becomes an admin. The owner only.
type TransferOwnership struct {
	Actor          access.Actor
	TeamID, UserID string
}

type TransferOwnershipHandler struct{ base }

func (h TransferOwnershipHandler) Handle(ctx context.Context, cmd TransferOwnership) error {
	if _, err := h.manager(ctx, cmd.TeamID, cmd.Actor, func(r access.Role) bool { return r == access.Owner }); err != nil {
		return err
	}
	if cmd.UserID == cmd.Actor.UserID {
		return domain.ErrCannotTransferToSelf
	}
	target, err := h.target(ctx, cmd.TeamID, cmd.UserID)
	if err != nil {
		return err
	}
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Members.SetRole(ctx, cmd.TeamID, target.UserID, access.Owner); err != nil {
			return err
		}
		if err := h.Members.SetRole(ctx, cmd.TeamID, cmd.Actor.UserID, access.Admin); err != nil {
			return err
		}
		return h.record(ctx, cmd.Actor, cmd.TeamID, "team.ownership_transferred", "member", target.UserID,
			map[string]string{"from": cmd.Actor.UserID, "to": target.UserID})
	})
}
