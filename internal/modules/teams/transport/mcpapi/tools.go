// Package mcpapi is the teams module's MCP transport: tools for teams,
// members and invitations. Each tool runs the same command or query as the
// matching REST endpoint and returns the same JSON shape.
package mcpapi

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/mcpx"
)

type tools struct {
	cmd command.Handlers
	qry query.Handlers
	now func() time.Time
}

// Register adds the teams, members and invitations tools to s.
func Register(s *mcp.Server, cmd command.Handlers, qry query.Handlers) {
	t := tools{cmd: cmd, qry: qry, now: time.Now}

	// ---- teams ----
	mcpx.Add(s, &mcp.Tool{
		Name:        "list_teams",
		Description: "List the teams the caller belongs to, each with the caller's role (owner, admin, member or viewer). Members are not included; get_team returns them.",
		Annotations: mcpx.Read("List teams"),
	}, t.listTeams)
	mcpx.Add(s, &mcp.Tool{
		Name:        "get_team",
		Description: "Get one of the caller's teams with the caller's role and every member (user ID, name, email, role, join date).",
		Annotations: mcpx.Read("Get team"),
	}, t.getTeam)
	mcpx.Add(s, &mcp.Tool{
		Name:        "create_team",
		Description: "Create a team with the caller as its owner. Returns the team with its members.",
		Annotations: mcpx.Write("Create team", false),
	}, t.createTeam)
	mcpx.Add(s, &mcp.Tool{
		Name:        "rename_team",
		Description: "Rename a team. Needs the admin role or above. Returns the team with its members.",
		Annotations: mcpx.Write("Rename team", true),
	}, t.renameTeam)
	mcpx.Add(s, &mcp.Tool{
		Name: "delete_team",
		Description: "Delete a team: it and its members and invitations are gone at once; its projects, collections, todos " +
			"and audit log are deleted in the background (the returned operation; check it with get_operation). Owner only.",
		Annotations: mcpx.Destructive("Delete team and its data"),
	}, t.deleteTeam)

	// ---- members ----
	mcpx.Add(s, &mcp.Tool{
		Name: "change_member_role",
		Description: "Give another member of a team a new role: admin, member or viewer (ownership moves only with transfer_team_ownership). " +
			"The caller must outrank both the member's current and new role, and can't change their own role. Returns the team with its members.",
		Annotations: mcpx.Write("Change member role", true),
	}, t.changeRole)
	mcpx.Add(s, &mcp.Tool{
		Name: "remove_member",
		Description: "Remove another member from a team. Owners and admins only, and only members with a lower role than the caller's. " +
			"To leave a team yourself, use leave_team.",
		Annotations: mcpx.Destructive("Remove team member"),
	}, t.removeMember)
	mcpx.Add(s, &mcp.Tool{
		Name: "leave_team",
		Description: "Take the caller out of a team; they lose access to its projects, collections and todos. " +
			"The owner can't leave (transfer ownership or delete the team instead). To remove someone else, use remove_member.",
		Annotations: mcpx.Destructive("Leave team"),
	}, t.leaveTeam)
	mcpx.Add(s, &mcp.Tool{
		Name: "transfer_team_ownership",
		Description: "Make another member the owner of a team; the caller, the current owner, becomes an admin. " +
			"Owner only. Returns the team with its members.",
		Annotations: mcpx.Destructive("Transfer team ownership"),
	}, t.transferOwnership)

	// ---- invitations ----
	mcpx.Add(s, &mcp.Tool{
		Name: "list_invitations",
		Description: "List a team's pending invitations, including expired ones (expired true), with when each was last sent and when it can be resent. " +
			"Owners and admins only.",
		Annotations: mcpx.Read("List team invitations"),
	}, t.listInvitations)
	mcpx.Add(s, &mcp.Tool{
		Name: "invite_member",
		Description: "Email an invitation to join a team with a role below the caller's (owners and admins only). " +
			"Inviting an email with a pending invitation replaces it, subject to the same cooldown as resend_invitation. Returns the invitation.",
		Annotations: mcpx.Outbound("Invite team member"),
	}, t.invite)
	mcpx.Add(s, &mcp.Tool{
		Name: "resend_invitation",
		Description: "Email an existing invitation again with a new link (the old link stops working) and a fresh expiry; works on expired invitations too. " +
			"Owners and admins only, at most once per cooldown (see resendAvailableAt). Returns the invitation.",
		Annotations: mcpx.Outbound("Resend invitation"),
	}, t.resendInvitation)
	mcpx.Add(s, &mcp.Tool{
		Name:        "revoke_invitation",
		Description: "Cancel a team's pending invitation so its link stops working. Owners and admins only. To turn down an invitation sent to the caller, use decline_invitation.",
		Annotations: mcpx.Destructive("Revoke invitation"),
	}, t.revokeInvitation)
	mcpx.Add(s, &mcp.Tool{
		Name: "accept_invitation",
		Description: "Join a team with an invitation addressed to the caller's email, using the token from the invitation email's link. " +
			"Returns the team with its members.",
		Annotations: mcpx.Write("Accept invitation", false),
	}, t.acceptInvitation)
	mcpx.Add(s, &mcp.Tool{
		Name:        "decline_invitation",
		Description: "Turn down an invitation addressed to the caller's email, using the token from the invitation email's link. The invitation is used up.",
		Annotations: mcpx.Write("Decline invitation", false),
	}, t.declineInvitation)
}

// ---- inputs ----

type teamInput struct {
	TeamID string `json:"teamId" jsonschema:"the team's ID"`
}

type nameInput struct {
	Name string `json:"name" jsonschema:"the team's name, up to 100 characters"`
}

type renameInput struct {
	TeamID string `json:"teamId" jsonschema:"the team's ID"`
	Name   string `json:"name" jsonschema:"the new name, up to 100 characters"`
}

type memberInput struct {
	TeamID string `json:"teamId" jsonschema:"the team's ID"`
	UserID string `json:"userId" jsonschema:"the member's user ID (from get_team)"`
}

type changeRoleInput struct {
	TeamID string `json:"teamId" jsonschema:"the team's ID"`
	UserID string `json:"userId" jsonschema:"the member's user ID (from get_team)"`
	Role   string `json:"role" jsonschema:"the new role: admin, member or viewer"`
}

type inviteInput struct {
	TeamID string `json:"teamId" jsonschema:"the team's ID"`
	Email  string `json:"email" jsonschema:"the email address to invite"`
	Role   string `json:"role" jsonschema:"the role on joining: admin, member or viewer, below the caller's"`
}

type invitationInput struct {
	TeamID       string `json:"teamId" jsonschema:"the team's ID"`
	InvitationID string `json:"invitationId" jsonschema:"the invitation's ID (from list_invitations)"`
}

type tokenInput struct {
	Token string `json:"token" jsonschema:"the token from the invitation email's link (its token query parameter)"`
}

// ---- outputs ----

type teamList struct {
	Teams []httpapi.TeamResponse `json:"teams"`
}

type invitationList struct {
	Invitations []httpapi.InvitationResponse `json:"invitations"`
}

type left struct {
	TeamID string `json:"teamId" jsonschema:"the team the caller left"`
	Left   bool   `json:"left"`
}

type declined struct {
	Declined bool `json:"declined"`
}

// ---- teams ----

func (t tools) readTeam(ctx context.Context, a access.Actor, id string) (httpapi.TeamDetailResponse, error) {
	d, err := t.qry.GetTeam.Handle(ctx, query.GetTeam{Actor: a, TeamID: id})
	if err != nil {
		return httpapi.TeamDetailResponse{}, err
	}
	return httpapi.ToTeamDetail(d), nil
}

func (t tools) listTeams(ctx context.Context, a access.Actor, _ struct{}) (teamList, error) {
	views, err := t.qry.ListTeams.Handle(ctx, query.ListTeams{Actor: a})
	if err != nil {
		return teamList{}, err
	}
	out := teamList{Teams: make([]httpapi.TeamResponse, 0, len(views))}
	for _, v := range views {
		out.Teams = append(out.Teams, httpapi.ToTeam(v))
	}
	return out, nil
}

func (t tools) getTeam(ctx context.Context, a access.Actor, in teamInput) (httpapi.TeamDetailResponse, error) {
	return t.readTeam(ctx, a, in.TeamID)
}

func (t tools) createTeam(ctx context.Context, a access.Actor, in nameInput) (httpapi.TeamDetailResponse, error) {
	id, err := t.cmd.CreateTeam.Handle(ctx, command.CreateTeam{Actor: a, Name: in.Name})
	if err != nil {
		return httpapi.TeamDetailResponse{}, err
	}
	return t.readTeam(ctx, a, id)
}

func (t tools) renameTeam(ctx context.Context, a access.Actor, in renameInput) (httpapi.TeamDetailResponse, error) {
	if err := t.cmd.RenameTeam.Handle(ctx, command.RenameTeam{Actor: a, TeamID: in.TeamID, Name: in.Name}); err != nil {
		return httpapi.TeamDetailResponse{}, err
	}
	return t.readTeam(ctx, a, in.TeamID)
}

// deletedTeam: the team is gone; its content is being deleted in the
// background (the operation).
type deletedTeam struct {
	ID        string         `json:"id" jsonschema:"the deleted team's ID"`
	Deleted   bool           `json:"deleted"`
	Operation jobs.Operation `json:"operation" jsonschema:"the background deletion of its projects, todos and audit log; see get_operation"`
}

func (t tools) deleteTeam(ctx context.Context, a access.Actor, in teamInput) (deletedTeam, error) {
	opID, err := t.cmd.DeleteTeam.Handle(ctx, command.DeleteTeam{Actor: a, TeamID: in.TeamID})
	if err != nil {
		return deletedTeam{}, err
	}
	accepted := jobs.NewAccepted(opID)
	return deletedTeam{ID: in.TeamID, Deleted: true, Operation: jobs.Operation{
		ID: opID, Kind: command.JobPurgeTeam, Status: accepted.Operation.Status, URL: accepted.Operation.URL,
	}}, nil
}

// ---- members ----

func (t tools) changeRole(ctx context.Context, a access.Actor, in changeRoleInput) (httpapi.TeamDetailResponse, error) {
	role, err := access.ParseRole(in.Role)
	if err != nil {
		return httpapi.TeamDetailResponse{}, err
	}
	if err := t.cmd.ChangeRole.Handle(ctx, command.ChangeRole{Actor: a, TeamID: in.TeamID, UserID: in.UserID, Role: role}); err != nil {
		return httpapi.TeamDetailResponse{}, err
	}
	return t.readTeam(ctx, a, in.TeamID)
}

func (t tools) removeMember(ctx context.Context, a access.Actor, in memberInput) (mcpx.Deleted, error) {
	if err := t.cmd.RemoveMember.Handle(ctx, command.RemoveMember{Actor: a, TeamID: in.TeamID, UserID: in.UserID}); err != nil {
		return mcpx.Deleted{}, err
	}
	return mcpx.Deleted{ID: in.UserID, Deleted: true}, nil
}

// leaveTeam is DELETE /teams/{id}/members/{userId} with the caller's own ID.
func (t tools) leaveTeam(ctx context.Context, a access.Actor, in teamInput) (left, error) {
	if err := t.cmd.RemoveMember.Handle(ctx, command.RemoveMember{Actor: a, TeamID: in.TeamID, UserID: a.UserID}); err != nil {
		return left{}, err
	}
	return left{TeamID: in.TeamID, Left: true}, nil
}

func (t tools) transferOwnership(ctx context.Context, a access.Actor, in memberInput) (httpapi.TeamDetailResponse, error) {
	if in.UserID == "" {
		return httpapi.TeamDetailResponse{}, apperrors.Validation("INVALID_USER_ID", "userId is required")
	}
	if err := t.cmd.TransferOwnership.Handle(ctx, command.TransferOwnership{Actor: a, TeamID: in.TeamID, UserID: in.UserID}); err != nil {
		return httpapi.TeamDetailResponse{}, err
	}
	return t.readTeam(ctx, a, in.TeamID)
}

// ---- invitations ----

func (t tools) readInvitation(ctx context.Context, a access.Actor, teamID, id string) (httpapi.InvitationResponse, error) {
	inv, err := t.qry.GetInvitation.Handle(ctx, query.GetInvitation{Actor: a, TeamID: teamID, ID: id})
	if err != nil {
		return httpapi.InvitationResponse{}, err
	}
	return httpapi.ToInvitation(inv, t.now()), nil
}

func (t tools) listInvitations(ctx context.Context, a access.Actor, in teamInput) (invitationList, error) {
	list, err := t.qry.ListInvitations.Handle(ctx, query.ListInvitations{Actor: a, TeamID: in.TeamID})
	if err != nil {
		return invitationList{}, err
	}
	now := t.now()
	out := invitationList{Invitations: make([]httpapi.InvitationResponse, 0, len(list))}
	for _, inv := range list {
		out.Invitations = append(out.Invitations, httpapi.ToInvitation(inv, now))
	}
	return out, nil
}

func (t tools) invite(ctx context.Context, a access.Actor, in inviteInput) (httpapi.InvitationResponse, error) {
	role, err := access.ParseRole(in.Role)
	if err != nil {
		return httpapi.InvitationResponse{}, err
	}
	id, err := t.cmd.Invite.Handle(ctx, command.Invite{Actor: a, TeamID: in.TeamID, Email: in.Email, Role: role})
	if err != nil {
		return httpapi.InvitationResponse{}, err
	}
	return t.readInvitation(ctx, a, in.TeamID, id)
}

func (t tools) resendInvitation(ctx context.Context, a access.Actor, in invitationInput) (httpapi.InvitationResponse, error) {
	err := t.cmd.ResendInvitation.Handle(ctx, command.ResendInvitation{Actor: a, TeamID: in.TeamID, ID: in.InvitationID})
	if err != nil {
		return httpapi.InvitationResponse{}, err
	}
	return t.readInvitation(ctx, a, in.TeamID, in.InvitationID)
}

func (t tools) revokeInvitation(ctx context.Context, a access.Actor, in invitationInput) (mcpx.Deleted, error) {
	if err := t.cmd.RevokeInvitation.Handle(ctx, command.RevokeInvitation{Actor: a, TeamID: in.TeamID, ID: in.InvitationID}); err != nil {
		return mcpx.Deleted{}, err
	}
	return mcpx.Deleted{ID: in.InvitationID, Deleted: true}, nil
}

func (t tools) acceptInvitation(ctx context.Context, a access.Actor, in tokenInput) (httpapi.TeamDetailResponse, error) {
	teamID, err := t.cmd.AcceptInvitation.Handle(ctx, command.AcceptInvitation{Actor: a, Token: in.Token})
	if err != nil {
		return httpapi.TeamDetailResponse{}, err
	}
	return t.readTeam(ctx, a, teamID)
}

func (t tools) declineInvitation(ctx context.Context, a access.Actor, in tokenInput) (declined, error) {
	if err := t.cmd.DeclineInvitation.Handle(ctx, command.DeclineInvitation{Actor: a, Token: in.Token}); err != nil {
		return declined{}, err
	}
	return declined{Declined: true}, nil
}
