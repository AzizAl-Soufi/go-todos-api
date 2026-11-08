// Package httpapi is the teams module's HTTP transport (driving adapter):
// /teams and /invitations. Each endpoint decodes the request, runs one
// command and reads the response back with a query.
package httpapi

import (
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
)

type handler struct {
	cmd command.Handlers
	qry query.Handlers
	now func() time.Time
}

type endpoint = func(r *http.Request, a access.Actor) (int, any, error)

// Register mounts the routes. protect must authenticate the caller and
// resolve them into an access.Actor; limit rate-limits the token endpoints.
func Register(mux *http.ServeMux, cmd command.Handlers, qry query.Handlers, protect, limit httpx.Middleware) {
	h := handler{cmd: cmd, qry: qry, now: time.Now}
	routes := map[string]endpoint{
		"POST /teams":        h.createTeam,
		"GET /teams":         h.listTeams,
		"GET /teams/{id}":    h.getTeam,
		"PATCH /teams/{id}":  h.renameTeam,
		"DELETE /teams/{id}": h.deleteTeam,

		"PATCH /teams/{id}/members/{userId}":  h.changeRole,
		"DELETE /teams/{id}/members/{userId}": h.removeMember,
		"POST /teams/{id}/transfer-ownership": h.transferOwnership,

		"POST /teams/{id}/invitations":                       h.invite,
		"POST /teams/{id}/invitations/{invitationId}/resend": h.resendInvitation,
		"GET /teams/{id}/invitations":                        h.listInvitations,
		"DELETE /teams/{id}/invitations/{invitationId}":      h.revokeInvitation,
	}
	for pattern, fn := range routes {
		mux.Handle(pattern, protect(access.ForActor(fn)))
	}
	// Rate-limited: they take a secret token.
	mux.Handle("POST /invitations/accept", limit(protect(access.ForActor(h.acceptInvitation))))
	mux.Handle("POST /invitations/decline", limit(protect(access.ForActor(h.declineInvitation))))
}

// ---- response shapes ----

// TeamResponse is a team with the caller's role in it.
type TeamResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ToTeam converts a team view into its response shape.
func ToTeam(v query.TeamView) TeamResponse {
	return TeamResponse{ID: v.ID, Name: v.Name, Role: string(v.Role), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

// MemberResponse is one member of a team.
type MemberResponse struct {
	UserID   string    `json:"userId"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
}

// TeamDetailResponse is a team with its members.
type TeamDetailResponse struct {
	TeamResponse
	Members []MemberResponse `json:"members"`
}

// ToTeamDetail converts a team with its members into its response shape.
func ToTeamDetail(d query.TeamDetail) TeamDetailResponse {
	out := TeamDetailResponse{TeamResponse: ToTeam(d.TeamView), Members: make([]MemberResponse, 0, len(d.Members))}
	for _, m := range d.Members {
		out.Members = append(out.Members, MemberResponse{UserID: m.UserID, Name: m.Name, Email: m.Email, Role: string(m.Role), JoinedAt: m.JoinedAt})
	}
	return out
}

// InvitationResponse is a pending (or expired) invitation.
type InvitationResponse struct {
	ID        string    `json:"id"`
	TeamID    string    `json:"teamId"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	InvitedBy string    `json:"invitedBy"`
	Expired   bool      `json:"expired"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
	// SentAt is when the invitation email was last sent; it may be sent
	// again (POST .../resend) from ResendAvailableAt on. Both in UTC.
	SentAt            time.Time `json:"sentAt"`
	ResendAvailableAt time.Time `json:"resendAvailableAt"`
}

// ToInvitation converts an invitation into its response shape; now decides
// whether it has expired.
func ToInvitation(i *domain.Invitation, now time.Time) InvitationResponse {
	return InvitationResponse{
		ID: i.ID, TeamID: i.TeamID, Email: i.Email, Role: string(i.Role), InvitedBy: i.InvitedBy,
		Expired: i.Expired(now), ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt,
		SentAt: i.SentAt.UTC(), ResendAvailableAt: i.ResendAvailableAt().UTC(),
	}
}

func (h handler) toInvitation(i *domain.Invitation) InvitationResponse {
	return ToInvitation(i, h.now())
}

func respond[T, R any](status int, v T, err error, toResponse func(T) R) (int, any, error) {
	if err != nil {
		return 0, nil, err
	}
	return status, toResponse(v), nil
}

func respondList[T, R any](items []T, err error, toResponse func(T) R) (int, any, error) {
	if err != nil {
		return 0, nil, err
	}
	out := make([]R, 0, len(items))
	for _, it := range items {
		out = append(out, toResponse(it))
	}
	return http.StatusOK, out, nil
}

// ---- requests ----

// Bodies are decoded into these transport-owned types and copied into
// commands explicitly.
type (
	nameRequest struct {
		Name string `json:"name"`
	}
	roleRequest struct {
		Role string `json:"role"`
	}
	inviteRequest struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	userRequest struct {
		UserID string `json:"userId"`
	}
	tokenRequest struct {
		Token string `json:"token"`
	}
)

func decode[T any](r *http.Request) (T, error) {
	var req T
	err := httpx.DecodeJSON(r, &req)
	return req, err
}

// ---- teams ----

func (h handler) readTeam(r *http.Request, status int, a access.Actor, id string) (int, any, error) {
	d, err := h.qry.GetTeam.Handle(r.Context(), query.GetTeam{Actor: a, TeamID: id})
	return respond(status, d, err, ToTeamDetail)
}

func (h handler) createTeam(r *http.Request, a access.Actor) (int, any, error) {
	req, err := decode[nameRequest](r)
	if err != nil {
		return 0, nil, err
	}
	id, err := h.cmd.CreateTeam.Handle(r.Context(), command.CreateTeam{Actor: a, Name: req.Name})
	if err != nil {
		return 0, nil, err
	}
	return h.readTeam(r, http.StatusCreated, a, id)
}

func (h handler) listTeams(r *http.Request, a access.Actor) (int, any, error) {
	views, err := h.qry.ListTeams.Handle(r.Context(), query.ListTeams{Actor: a})
	return respondList(views, err, ToTeam)
}

func (h handler) getTeam(r *http.Request, a access.Actor) (int, any, error) {
	return h.readTeam(r, http.StatusOK, a, r.PathValue("id"))
}

func (h handler) renameTeam(r *http.Request, a access.Actor) (int, any, error) {
	req, err := decode[nameRequest](r)
	if err != nil {
		return 0, nil, err
	}
	id := r.PathValue("id")
	if err := h.cmd.RenameTeam.Handle(r.Context(), command.RenameTeam{Actor: a, TeamID: id, Name: req.Name}); err != nil {
		return 0, nil, err
	}
	return h.readTeam(r, http.StatusOK, a, id)
}

// deleteTeam answers 202: the team is gone, and its content is being
// deleted in the background (GET /operations/{id}).
func (h handler) deleteTeam(r *http.Request, a access.Actor) (int, any, error) {
	opID, err := h.cmd.DeleteTeam.Handle(r.Context(), command.DeleteTeam{Actor: a, TeamID: r.PathValue("id")})
	if err != nil {
		return 0, nil, err
	}
	return http.StatusAccepted, jobs.NewAccepted(opID), nil
}

// ---- members ----

func (h handler) changeRole(r *http.Request, a access.Actor) (int, any, error) {
	req, err := decode[roleRequest](r)
	if err != nil {
		return 0, nil, err
	}
	role, err := access.ParseRole(req.Role)
	if err != nil {
		return 0, nil, err
	}
	id := r.PathValue("id")
	cmd := command.ChangeRole{Actor: a, TeamID: id, UserID: r.PathValue("userId"), Role: role}
	if err := h.cmd.ChangeRole.Handle(r.Context(), cmd); err != nil {
		return 0, nil, err
	}
	return h.readTeam(r, http.StatusOK, a, id)
}

func (h handler) removeMember(r *http.Request, a access.Actor) (int, any, error) {
	cmd := command.RemoveMember{Actor: a, TeamID: r.PathValue("id"), UserID: r.PathValue("userId")}
	return http.StatusNoContent, nil, h.cmd.RemoveMember.Handle(r.Context(), cmd)
}

func (h handler) transferOwnership(r *http.Request, a access.Actor) (int, any, error) {
	req, err := decode[userRequest](r)
	if err != nil {
		return 0, nil, err
	}
	if req.UserID == "" {
		return 0, nil, apperrors.Validation("INVALID_USER_ID", "userId is required")
	}
	id := r.PathValue("id")
	if err := h.cmd.TransferOwnership.Handle(r.Context(), command.TransferOwnership{Actor: a, TeamID: id, UserID: req.UserID}); err != nil {
		return 0, nil, err
	}
	return h.readTeam(r, http.StatusOK, a, id)
}

// ---- invitations ----

func (h handler) invite(r *http.Request, a access.Actor) (int, any, error) {
	req, err := decode[inviteRequest](r)
	if err != nil {
		return 0, nil, err
	}
	role, err := access.ParseRole(req.Role)
	if err != nil {
		return 0, nil, err
	}
	teamID := r.PathValue("id")
	id, err := h.cmd.Invite.Handle(r.Context(), command.Invite{Actor: a, TeamID: teamID, Email: req.Email, Role: role})
	if err != nil {
		return 0, nil, err
	}
	inv, err := h.qry.GetInvitation.Handle(r.Context(), query.GetInvitation{Actor: a, TeamID: teamID, ID: id})
	return respond(http.StatusCreated, inv, err, h.toInvitation)
}

func (h handler) resendInvitation(r *http.Request, a access.Actor) (int, any, error) {
	teamID, id := r.PathValue("id"), r.PathValue("invitationId")
	err := h.cmd.ResendInvitation.Handle(r.Context(), command.ResendInvitation{Actor: a, TeamID: teamID, ID: id})
	if err != nil {
		return 0, nil, err
	}
	inv, err := h.qry.GetInvitation.Handle(r.Context(), query.GetInvitation{Actor: a, TeamID: teamID, ID: id})
	return respond(http.StatusOK, inv, err, h.toInvitation)
}

func (h handler) listInvitations(r *http.Request, a access.Actor) (int, any, error) {
	list, err := h.qry.ListInvitations.Handle(r.Context(), query.ListInvitations{Actor: a, TeamID: r.PathValue("id")})
	return respondList(list, err, h.toInvitation)
}

func (h handler) revokeInvitation(r *http.Request, a access.Actor) (int, any, error) {
	cmd := command.RevokeInvitation{Actor: a, TeamID: r.PathValue("id"), ID: r.PathValue("invitationId")}
	return http.StatusNoContent, nil, h.cmd.RevokeInvitation.Handle(r.Context(), cmd)
}

func (h handler) acceptInvitation(r *http.Request, a access.Actor) (int, any, error) {
	req, err := decode[tokenRequest](r)
	if err != nil {
		return 0, nil, err
	}
	teamID, err := h.cmd.AcceptInvitation.Handle(r.Context(), command.AcceptInvitation{Actor: a, Token: req.Token})
	if err != nil {
		return 0, nil, err
	}
	return h.readTeam(r, http.StatusOK, a, teamID)
}

func (h handler) declineInvitation(r *http.Request, a access.Actor) (int, any, error) {
	req, err := decode[tokenRequest](r)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.cmd.DeclineInvitation.Handle(r.Context(), command.DeclineInvitation{Actor: a, Token: req.Token})
}
