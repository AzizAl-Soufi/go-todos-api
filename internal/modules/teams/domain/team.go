// Package domain holds the teams module's entities (teams, members,
// invitations) and the rules for who may change whose role. Roles and what
// they allow are defined in platform/access, shared with the modules that
// authorize team content.
package domain

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

const (
	MaxNameLength  = 100
	MaxEmailLength = 254

	// InvitationResendCooldown is how long after an invitation email was
	// sent before it may be sent again, so an invitee isn't flooded.
	InvitationResendCooldown = 2 * time.Minute
)

var (
	ErrInvalidName           = apperrors.Validation("INVALID_TEAM_NAME", "team name is required")
	ErrNameTooLong           = apperrors.Validationf("TEAM_NAME_TOO_LONG", "team name must be at most %d characters", MaxNameLength)
	ErrInvalidEmail          = apperrors.Validation("INVALID_EMAIL", "a valid email address is required")
	ErrMemberNotFound        = apperrors.NotFound("MEMBER_NOT_FOUND", "member not found")
	ErrInvitationNotFound    = apperrors.NotFound("INVITATION_NOT_FOUND", "invitation not found")
	ErrInvalidInvitation     = apperrors.Validation("INVALID_INVITATION_TOKEN", "invitation is invalid or has expired")
	ErrInvitationForOther    = apperrors.Forbidden("INVITATION_EMAIL_MISMATCH", "this invitation was sent to a different email address")
	ErrAlreadyMember         = apperrors.Conflict("ALREADY_MEMBER", "this person is already a member of the team")
	ErrOwnerNotAssignable    = apperrors.Validation("OWNER_NOT_ASSIGNABLE", "ownership can only be transferred (POST /teams/{id}/transfer-ownership)")
	ErrOwnerCannotLeave      = apperrors.Conflict("OWNER_CANNOT_LEAVE", "the owner must transfer ownership or delete the team before leaving")
	ErrCannotChangeOwnRole   = apperrors.Conflict("CANNOT_CHANGE_OWN_ROLE", "you can't change your own role")
	ErrCannotTransferToSelf  = apperrors.Conflict("CANNOT_TRANSFER_TO_SELF", "you already own this team")
	ErrOwnsTeamsWithMembers  = apperrors.Conflict("OWNS_TEAMS", "transfer ownership of your teams that have other members (or delete them) before deleting your account")
	ErrResendTooSoon         = apperrors.TooManyRequests("INVITATION_RESEND_TOO_SOON", "an invitation was sent to this address less than 2 minutes ago; try again after retryAt")
	ErrInvitationRoleTooHigh = apperrors.Forbidden("INSUFFICIENT_ROLE", "you can only invite people with a role below your own")
)

type Team struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewTeam(name string, now time.Time) (*Team, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	now = Timestamp(now)
	return &Team{Name: name, CreatedAt: now, UpdatedAt: now}, nil
}

func (t *Team) Rename(name string, now time.Time) error {
	name, err := NormalizeName(name)
	if err != nil {
		return err
	}
	t.Name, t.UpdatedAt = name, Timestamp(now)
	return nil
}

func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", ErrInvalidName
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", ErrNameTooLong
	}
	return name, nil
}

// Member is a user's membership in a team. Name and email are what the user
// had when they joined (the teams module doesn't read accounts).
type Member struct {
	TeamID   string
	UserID   string
	Name     string
	Email    string
	Role     access.Role
	JoinedAt time.Time
}

// Invitation is a pending invitation for an email address. Only the hash of
// its token is stored.
type Invitation struct {
	ID        string
	TeamID    string
	Email     string
	Role      access.Role
	InvitedBy string // user ID
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	// SentAt is when the invitation email was last sent (created or resent).
	SentAt time.Time
}

func (i *Invitation) Expired(now time.Time) bool { return !now.Before(i.ExpiresAt) }

// ResendAvailableAt is when the invitation email may be sent again.
func (i *Invitation) ResendAvailableAt() time.Time {
	return i.SentAt.Add(InvitationResendCooldown)
}

// CheckResend fails with ErrResendTooSoon, retryable from
// ResendAvailableAt, until the cooldown has passed.
func (i *Invitation) CheckResend(now time.Time) error {
	if at := i.ResendAvailableAt(); now.Before(at) {
		return apperrors.RetryAt(ErrResendTooSoon, now, at)
	}
	return nil
}

// NormalizeEmail trims and lower-cases an address and requires a bare
// "local@domain" form, like account emails.
func NormalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(email) > MaxEmailLength {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", ErrInvalidEmail
	}
	return email, nil
}

// ---- who may do what to whom ----
//
// Owners and admins manage the team; a manager only acts on people strictly
// below them and only hands out roles strictly below their own. Ownership is
// never assigned, only transferred by the owner.

// CanInvite checks that actor may invite someone as role.
func CanInvite(actor, role access.Role) error {
	switch {
	case role == access.Owner:
		return ErrOwnerNotAssignable
	case !actor.CanManage():
		return access.ErrInsufficientRole
	case !actor.Outranks(role):
		return ErrInvitationRoleTooHigh
	}
	return nil
}

// CanChangeRole checks that actor may move a member from current to next.
func CanChangeRole(actor, current, next access.Role) error {
	switch {
	case next == access.Owner:
		return ErrOwnerNotAssignable
	case !actor.CanManage() || !actor.Outranks(current) || !actor.Outranks(next):
		return access.ErrInsufficientRole
	}
	return nil
}

// CanRemove checks that actor may remove a member with role target.
func CanRemove(actor, target access.Role) error {
	if !actor.CanManage() || !actor.Outranks(target) {
		return access.ErrInsufficientRole
	}
	return nil
}

// Timestamp normalizes a time to UTC milliseconds, which every backend keeps.
func Timestamp(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }
