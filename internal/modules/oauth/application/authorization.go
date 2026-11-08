package application

import (
	"context"
	"errors"
	"strings"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// Config names this server in OAuth terms.
type Config struct {
	// Issuer is the authorization server's identifier, its base URL.
	Issuer string
	// Resource is the MCP endpoint's URL: the audience every token is bound
	// to (RFC 8707).
	Resource string
}

// Clients finds clients: registered ones in the store, and metadata
// document clients (an HTTPS URL as client ID) by fetching the document.
type Clients struct {
	Repo     ClientReader
	Metadata ClientMetadataFetcher
}

// Resolve returns the client with id; domain.ErrInvalidClient when there is
// no such client.
func (c Clients) Resolve(ctx context.Context, id string) (*domain.Client, error) {
	if id == "" {
		return nil, domain.ErrInvalidClient
	}
	if strings.HasPrefix(id, "https://") {
		if c.Metadata == nil {
			return nil, domain.ErrInvalidClient
		}
		return c.Metadata.Fetch(ctx, id)
	}
	client, err := c.Repo.Get(ctx, id)
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrInvalidID) {
		return nil, domain.ErrInvalidClient
	}
	return client, err
}

// RedirectError is an authorization failure to report at the client's
// redirect URI (RFC 6749 section 4.1.2.1): the client and redirect URI were
// valid, the rest of the request wasn't. Other failures must not redirect.
type RedirectError struct {
	Err         *domain.Error
	RedirectURI string
	State       string
}

func (e *RedirectError) Error() string { return e.Err.Error() }
func (e *RedirectError) Unwrap() error { return e.Err }

// CheckAuthorization validates an authorization request: the client must
// exist and own the redirect URI (otherwise a plain error, never a
// redirect), then the rest (a RedirectError). It returns the client and the
// normalized request.
func CheckAuthorization(ctx context.Context, clients Clients, cfg Config, req domain.AuthorizationRequest) (*domain.Client, domain.AuthorizationRequest, error) {
	client, err := clients.Resolve(ctx, req.ClientID)
	if err != nil {
		return nil, req, err
	}
	if req.RedirectURI == "" || !client.AllowsRedirect(req.RedirectURI) {
		return nil, req, domain.ErrInvalidRedirectURI
	}
	checked, err := req.Check(cfg.Resource)
	var oauthErr *domain.Error
	if errors.As(err, &oauthErr) {
		return nil, req, &RedirectError{Err: oauthErr, RedirectURI: req.RedirectURI, State: req.State}
	}
	return client, checked, err
}
