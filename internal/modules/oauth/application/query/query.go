// Package query holds the oauth module's read use cases.
package query

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/opaque"
)

type Deps struct {
	Clients  application.ClientReader
	Grants   application.GrantReader
	Metadata application.ClientMetadataFetcher // nil: no metadata document clients
	Config   application.Config
	Now      func() time.Time // nil means time.Now
}

type Handlers struct {
	CheckAuthorization CheckAuthorizationHandler
	AuthenticateToken  AuthenticateTokenHandler
	ListConnections    ListConnectionsHandler
	GetClient          GetClientHandler
}

func New(d Deps) Handlers {
	if d.Now == nil {
		d.Now = time.Now
	}
	return Handlers{
		CheckAuthorization: CheckAuthorizationHandler{d},
		AuthenticateToken:  AuthenticateTokenHandler{d},
		ListConnections:    ListConnectionsHandler{d},
		GetClient:          GetClientHandler{d},
	}
}

// CheckAuthorization validates an authorization request for the sign-in
// page: the client that asks, and the normalized request to approve.
type CheckAuthorization struct {
	Request domain.AuthorizationRequest
}

type Authorization struct {
	Client  *domain.Client
	Request domain.AuthorizationRequest
}

type CheckAuthorizationHandler struct{ Deps }

func (h CheckAuthorizationHandler) Handle(ctx context.Context, q CheckAuthorization) (Authorization, error) {
	clients := application.Clients{Repo: h.Clients, Metadata: h.Metadata}
	client, req, err := application.CheckAuthorization(ctx, clients, h.Config, q.Request)
	return Authorization{Client: client, Request: req}, err
}

// ErrInvalidToken is any access token that doesn't authenticate: unknown,
// expired, revoked, or for another resource. The caller learns nothing more.
var ErrInvalidToken = errors.New("invalid access token")

// AuthenticateToken resolves an MCP access token into the account that
// granted it.
type AuthenticateToken struct {
	Token string
}

// Authenticated is a valid access token's owner and limits.
type Authenticated struct {
	Principal auth.Principal
	Scopes    []string
	ExpiresAt time.Time
	ClientID  string
}

type AuthenticateTokenHandler struct{ Deps }

func (h AuthenticateTokenHandler) Handle(ctx context.Context, q AuthenticateToken) (Authenticated, error) {
	grantID, secret, ok := domain.ParseToken(domain.AccessToken, q.Token)
	if !ok {
		return Authenticated{}, ErrInvalidToken
	}
	grant, err := h.Grants.Get(ctx, grantID)
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrInvalidID) {
		return Authenticated{}, ErrInvalidToken
	}
	if err != nil {
		return Authenticated{}, err
	}
	expired := !h.Now().Before(grant.AccessExpiresAt)
	wrongAudience := !domain.SameResource(grant.Resource, h.Config.Resource)
	if expired || wrongAudience || !domain.SameHash(grant.AccessHash, opaque.Hash(secret)) {
		return Authenticated{}, ErrInvalidToken
	}
	o := grant.Owner
	return Authenticated{
		Principal: auth.Principal{UserID: o.UserID, Name: o.Name, Email: o.Email, EmailVerified: o.EmailVerified},
		Scopes:    strings.Fields(grant.Scope),
		ExpiresAt: grant.AccessExpiresAt,
		ClientID:  grant.ClientID,
	}, nil
}

// ListConnections returns the apps the user connected, most recently used
// first.
type ListConnections struct {
	UserID string
}

type ListConnectionsHandler struct{ Deps }

func (h ListConnectionsHandler) Handle(ctx context.Context, q ListConnections) ([]*domain.Grant, error) {
	return h.Grants.ListForUser(ctx, q.UserID)
}

// GetClient returns a registered client (for the registration response).
type GetClient struct {
	ID string
}

type GetClientHandler struct{ Deps }

func (h GetClientHandler) Handle(ctx context.Context, q GetClient) (*domain.Client, error) {
	return h.Clients.Get(ctx, q.ID)
}
