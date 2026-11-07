// Package query holds the teams module's read use cases. Non-members get
// access.ErrTeamNotFound for every team read, so team IDs don't leak.
package query

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

type Deps struct {
	Teams       application.TeamReader
	Members     application.MemberReader
	Invitations application.InvitationReader
}

type Handlers struct {
	ListTeams       ListTeamsHandler
	GetTeam         GetTeamHandler
	ListInvitations ListInvitationsHandler
	GetInvitation   GetInvitationHandler
	Memberships     MembershipsHandler
	Roster          RosterHandler
}

func New(d Deps) Handlers {
	return Handlers{
		ListTeams: ListTeamsHandler{d}, GetTeam: GetTeamHandler{d},
		ListInvitations: ListInvitationsHandler{d}, GetInvitation: GetInvitationHandler{d},
		Memberships: MembershipsHandler{d}, Roster: RosterHandler{d},
	}
}

// TeamView is a team with the caller's role in it.
type TeamView struct {
	*domain.Team
	Role access.Role
}

// TeamDetail is a team with the caller's role and every member.
type TeamDetail struct {
	TeamView
	Members []*domain.Member
}

// role returns the caller's stored role in a team.
func role(ctx context.Context, members application.MemberReader, teamID, userID string) (access.Role, error) {
	m, err := members.Get(ctx, teamID, userID)
	if err != nil {
		return "", apperrors.UnresolvedAs(err, access.ErrTeamNotFound)
	}
	return m.Role, nil
}

// ListTeams lists the caller's teams, earliest joined first.
type ListTeams struct {
	Actor access.Actor
}

type ListTeamsHandler struct{ Deps }

func (h ListTeamsHandler) Handle(ctx context.Context, q ListTeams) ([]TeamView, error) {
	memberships, err := h.Members.ListByUser(ctx, q.Actor.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]TeamView, 0, len(memberships))
	for _, m := range memberships {
		team, err := h.Teams.Get(ctx, m.TeamID)
		if err != nil {
			return nil, err
		}
		out = append(out, TeamView{Team: team, Role: m.Role})
	}
	return out, nil
}

type GetTeam struct {
	Actor  access.Actor
	TeamID string
}

type GetTeamHandler struct{ Deps }

func (h GetTeamHandler) Handle(ctx context.Context, q GetTeam) (TeamDetail, error) {
	r, err := role(ctx, h.Members, q.TeamID, q.Actor.UserID)
	if err != nil {
		return TeamDetail{}, err
	}
	team, err := h.Teams.Get(ctx, q.TeamID)
	if err != nil {
		return TeamDetail{}, apperrors.NotFoundAs(err, access.ErrTeamNotFound)
	}
	members, err := h.Members.ListByTeam(ctx, q.TeamID)
	if err != nil {
		return TeamDetail{}, err
	}
	return TeamDetail{TeamView: TeamView{Team: team, Role: r}, Members: members}, nil
}

// requireManager checks the caller may see invitations (owners and admins).
func requireManager(ctx context.Context, members application.MemberReader, teamID, userID string) error {
	r, err := role(ctx, members, teamID, userID)
	if err != nil {
		return err
	}
	if !r.CanManage() {
		return access.ErrInsufficientRole
	}
	return nil
}

// ListInvitations lists a team's pending invitations; owners and admins only.
type ListInvitations struct {
	Actor  access.Actor
	TeamID string
}

type ListInvitationsHandler struct{ Deps }

func (h ListInvitationsHandler) Handle(ctx context.Context, q ListInvitations) ([]*domain.Invitation, error) {
	if err := requireManager(ctx, h.Members, q.TeamID, q.Actor.UserID); err != nil {
		return nil, err
	}
	return h.Invitations.ListByTeam(ctx, q.TeamID)
}

type GetInvitation struct {
	Actor      access.Actor
	TeamID, ID string
}

type GetInvitationHandler struct{ Deps }

func (h GetInvitationHandler) Handle(ctx context.Context, q GetInvitation) (*domain.Invitation, error) {
	if err := requireManager(ctx, h.Members, q.TeamID, q.Actor.UserID); err != nil {
		return nil, err
	}
	inv, err := h.Invitations.Get(ctx, q.TeamID, q.ID)
	return inv, apperrors.UnresolvedAs(err, domain.ErrInvitationNotFound)
}

// MembershipsHandler resolves a user's roles per team; it implements
// access.Resolver for access.Middleware.
type MembershipsHandler struct{ Deps }

var _ access.Resolver = MembershipsHandler{}

func (h MembershipsHandler) Memberships(ctx context.Context, userID string) (map[string]access.Role, error) {
	list, err := h.Members.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]access.Role, len(list))
	for _, m := range list {
		out[m.TeamID] = m.Role
	}
	return out, nil
}

// RosterHandler returns a team's name and members' roles; it implements
// access.Directory for modules that address a whole team.
type RosterHandler struct{ Deps }

var _ access.Directory = RosterHandler{}

func (h RosterHandler) Roster(ctx context.Context, teamID string) (access.Roster, error) {
	team, err := h.Teams.Get(ctx, teamID)
	if err != nil {
		return access.Roster{}, apperrors.UnresolvedAs(err, access.ErrTeamNotFound)
	}
	list, err := h.Members.ListByTeam(ctx, teamID)
	if err != nil {
		return access.Roster{}, err
	}
	members := make(map[string]access.Role, len(list))
	for _, m := range list {
		members[m.UserID] = m.Role
	}
	return access.Roster{TeamID: team.ID, Name: team.Name, Members: members}, nil
}
