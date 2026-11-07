// Package access is the shared authorization vocabulary: team roles, what
// each role may do, and the Actor (the caller plus the teams they belong to)
// that modules authorize against.
//
// The teams module owns memberships and implements Resolver; other modules
// only see the Actor that Middleware puts in the request context, so they never
// import the teams module.
package access

import (
	"context"
	"maps"
	"net/http"
	"slices"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

// Role is a member's role in a team. Higher roles can do everything lower
// roles can.
type Role string

const (
	Owner  Role = "owner"  // everything, including deleting the team
	Admin  Role = "admin"  // manage members and invitations, delete content, read the audit log
	Member Role = "member" // create and edit projects, collections and todos
	Viewer Role = "viewer" // read only
)

// Roles lists every role, highest first.
var Roles = []Role{Owner, Admin, Member, Viewer}

var (
	ErrInvalidRole = apperrors.Validation("INVALID_ROLE", "role must be one of owner, admin, member, viewer")
	// ErrTeamNotFound is also what non-members get, so team IDs don't leak.
	ErrTeamNotFound     = apperrors.NotFound("TEAM_NOT_FOUND", "team not found")
	ErrInsufficientRole = apperrors.Forbidden("INSUFFICIENT_ROLE", "your role in this team does not allow this")
)

func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !slices.Contains(Roles, r) {
		return "", ErrInvalidRole
	}
	return r, nil
}

func (r Role) rank() int {
	if i := slices.Index(Roles, r); i >= 0 {
		return len(Roles) - i
	}
	return 0
}

// Outranks is true when r is strictly higher than o.
func (r Role) Outranks(o Role) bool { return r.rank() > o.rank() }

// CanEdit: create and change team projects, collections and todos.
func (r Role) CanEdit() bool { return r.rank() >= Member.rank() }

// CanManage: manage members and invitations, delete team projects and
// collections, read the team audit log.
func (r Role) CanManage() bool { return r.rank() >= Admin.rank() }

// Actor is an authenticated caller with their team memberships.
type Actor struct {
	UserID string
	Name   string
	Email  string
	Teams  map[string]Role // team ID -> role
}

// Personal is an actor that acts only on their own records (no teams).
func Personal(userID string) Actor { return Actor{UserID: userID} }

// Role returns the actor's role in teamID; ok is false for non-members.
func (a Actor) Role(teamID string) (Role, bool) {
	r, ok := a.Teams[teamID]
	return r, ok
}

// Require checks the actor's role in a team: non-members get
// ErrTeamNotFound, members whose role fails allowed get ErrInsufficientRole.
func (a Actor) Require(teamID string, allowed func(Role) bool) error {
	role, ok := a.Role(teamID)
	if !ok {
		return ErrTeamNotFound
	}
	if !allowed(role) {
		return ErrInsufficientRole
	}
	return nil
}

// TeamIDs returns the actor's teams, sorted.
func (a Actor) TeamIDs() []string { return slices.Sorted(maps.Keys(a.Teams)) }

// Roster is a team's name and its members' roles (user ID -> role).
type Roster struct {
	TeamID  string
	Name    string
	Members map[string]Role
}

// Directory looks up a team's roster; ErrTeamNotFound for an unknown team.
// Like Resolver, it is implemented by the teams module, so modules that need
// a team's members (notifications) don't import it.
type Directory interface {
	Roster(ctx context.Context, teamID string) (Roster, error)
}

// Resolver loads a user's memberships (team ID -> role).
type Resolver interface {
	Memberships(ctx context.Context, userID string) (map[string]Role, error)
}

type ctxKey struct{}

func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(ctxKey{}).(Actor)
	return a, ok && a.UserID != ""
}

// Middleware turns the authenticated principal (it must run after
// auth.Middleware) into an Actor with fresh memberships.
func Middleware(res Resolver) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.PrincipalFrom(r.Context())
			if !ok {
				httpx.RespondErr(w, r, auth.ErrNoPrincipal)
				return
			}
			teams, err := res.Memberships(r.Context(), p.UserID)
			if err != nil {
				httpx.RespondErr(w, r, err)
				return
			}
			a := Actor{UserID: p.UserID, Name: p.Name, Email: p.Email, Teams: teams}
			next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), a)))
		})
	}
}

// ForActor adapts an endpoint that needs the caller as an Actor; without
// one (route not behind Middleware) it answers 401.
func ForActor(fn func(r *http.Request, a Actor) (int, any, error)) httpx.Endpoint {
	return func(r *http.Request) (int, any, error) {
		a, ok := ActorFrom(r.Context())
		if !ok {
			return 0, nil, auth.ErrNoPrincipal
		}
		return fn(r, a)
	}
}
