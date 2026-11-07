// Package query holds the identity module's read use cases. Query handlers
// never change state and depend only on read ports.
package query

import (
	"context"
	"errors"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
)

type Deps struct {
	Users  application.UserReader
	Tokens application.AccessTokenVerifier
	Todos  application.TodoLister
}

// Handlers are all identity queries.
type Handlers struct {
	GetUser      GetUserHandler
	GetOverview  GetOverviewHandler
	Authenticate AuthenticateHandler
}

func New(d Deps) Handlers {
	return Handlers{GetUser: GetUserHandler{d}, GetOverview: GetOverviewHandler{d}, Authenticate: AuthenticateHandler{d}}
}

// GetUser returns one account.
type GetUser struct {
	UserID string
}

type GetUserHandler struct{ d Deps }

func (h GetUserHandler) Handle(ctx context.Context, q GetUser) (*domain.User, error) {
	return h.d.Users.FindByID(ctx, q.UserID)
}

// GetOverview returns the account with all of its todos.
type GetOverview struct {
	UserID string
}

type Overview struct {
	User  *domain.User
	Todos []application.TodoSummary
}

type GetOverviewHandler struct{ d Deps }

func (h GetOverviewHandler) Handle(ctx context.Context, q GetOverview) (Overview, error) {
	user, err := h.d.Users.FindByID(ctx, q.UserID)
	if err != nil {
		return Overview{}, err
	}
	todos, err := h.d.Todos.ListForUser(ctx, q.UserID)
	if err != nil {
		return Overview{}, err
	}
	return Overview{User: user, Todos: todos}, nil
}

// Authenticate resolves an access token to the caller. Besides the signature
// and expiry it checks, per request, that the user still exists and that the
// token version matches; that is what makes logout-all, password changes and
// account deletion take effect immediately.
type Authenticate struct {
	AccessToken string
}

type AuthenticateHandler struct{ d Deps }

var _ auth.Authenticator = AuthenticateHandler{}

func (h AuthenticateHandler) Handle(ctx context.Context, q Authenticate) (auth.Principal, error) {
	claims, err := h.d.Tokens.Parse(q.AccessToken)
	if err != nil {
		return auth.Principal{}, err
	}
	user, err := h.d.Users.FindByID(ctx, claims.UserID)
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrInvalidID) {
		return auth.Principal{}, domain.ErrInvalidAccessToken
	}
	if err != nil {
		return auth.Principal{}, err
	}
	if user.TokenVersion != claims.TokenVersion {
		return auth.Principal{}, domain.ErrTokenRevoked
	}
	return auth.Principal{UserID: user.ID, Name: user.Name, Email: user.Email, EmailVerified: user.EmailVerified()}, nil
}

// Authenticate implements auth.Authenticator for the HTTP middleware.
func (h AuthenticateHandler) Authenticate(ctx context.Context, accessToken string) (auth.Principal, error) {
	return h.Handle(ctx, Authenticate{AccessToken: accessToken})
}
