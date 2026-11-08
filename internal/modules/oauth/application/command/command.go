// Package command holds the oauth module's write use cases, one handler per
// command.
package command

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/opaque"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

type Deps struct {
	Clients  application.ClientRepository
	Codes    application.CodeRepository
	Grants   application.GrantRepository
	Metadata application.ClientMetadataFetcher // nil: no metadata document clients
	Accounts application.Authenticator
	Tx       tx.Runner
	Config   application.Config
	Now      func() time.Time // nil means time.Now
}

type Handlers struct {
	RegisterClient        RegisterClientHandler
	Approve               ApproveHandler
	ExchangeCode          ExchangeCodeHandler
	Refresh               RefreshHandler
	RevokeToken           RevokeTokenHandler
	RevokeConnection      RevokeConnectionHandler
	RevokeUserConnections RevokeUserConnectionsHandler
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
		RegisterClient:        RegisterClientHandler{b},
		Approve:               ApproveHandler{b},
		ExchangeCode:          ExchangeCodeHandler{b},
		Refresh:               RefreshHandler{b},
		RevokeToken:           RevokeTokenHandler{b},
		RevokeConnection:      RevokeConnectionHandler{b},
		RevokeUserConnections: RevokeUserConnectionsHandler{b},
	}
}

type base struct{ Deps }

func (b base) clients() application.Clients {
	return application.Clients{Repo: b.Clients, Metadata: b.Metadata}
}

// TokenSet is what the token endpoint returns.
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	Scope        string
}

// secrets are a new token pair for a grant: what the client gets (after
// the grant ID is known) and the hashes that are stored.
type secrets struct {
	access, accessHash   string
	refresh, refreshHash string
}

func newSecrets() (secrets, error) {
	access, accessHash, err := opaque.New()
	if err != nil {
		return secrets{}, err
	}
	refresh, refreshHash, err := opaque.New()
	if err != nil {
		return secrets{}, err
	}
	return secrets{access: access, accessHash: accessHash, refresh: refresh, refreshHash: refreshHash}, nil
}

func (s secrets) tokenSet(grantID, scope string) TokenSet {
	return TokenSet{
		AccessToken:  domain.FormatToken(domain.AccessToken, grantID, s.access),
		RefreshToken: domain.FormatToken(domain.RefreshToken, grantID, s.refresh),
		ExpiresIn:    domain.AccessTokenTTL,
		Scope:        scope,
	}
}

// ---- dynamic client registration ----

// RegisterClient registers a public client (RFC 7591). Only what this server
// supports is accepted: no client secret, the authorization code and
// refresh token grants, the code response type.
type RegisterClient struct {
	Name                    string
	RedirectURIs            []string
	TokenEndpointAuthMethod string
	GrantTypes              []string
	ResponseTypes           []string
}

type RegisterClientHandler struct{ base }

// Handle returns the new client's ID.
func (h RegisterClientHandler) Handle(ctx context.Context, cmd RegisterClient) (string, error) {
	if cmd.TokenEndpointAuthMethod != "" && cmd.TokenEndpointAuthMethod != "none" {
		return "", domain.ErrInvalidClientMetadata
	}
	for _, g := range cmd.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			return "", domain.ErrInvalidClientMetadata
		}
	}
	for _, r := range cmd.ResponseTypes {
		if r != "code" {
			return "", domain.ErrInvalidClientMetadata
		}
	}
	client, err := domain.NewClient(cmd.Name, cmd.RedirectURIs, domain.Registered, h.Now())
	if err != nil {
		return "", err
	}
	if err := h.Clients.Create(ctx, client); err != nil {
		return "", err
	}
	return client.ID, nil
}

// ---- authorization ----

// Approve records the user's consent to an authorization request and
// returns the authorization code for the client's redirect. The user proves
// who they are with a Firebase ID token from the sign-in page, so the
// approval can't be forged across sites without one.
type Approve struct {
	Request domain.AuthorizationRequest
	IDToken string
}

type ApproveHandler struct{ base }

func (h ApproveHandler) Handle(ctx context.Context, cmd Approve) (string, error) {
	client, req, err := application.CheckAuthorization(ctx, h.clients(), h.Config, cmd.Request)
	if err != nil {
		return "", err
	}
	principal, err := h.Accounts.Authenticate(ctx, cmd.IDToken)
	if err != nil {
		return "", err
	}
	code, hash, err := opaque.New()
	if err != nil {
		return "", err
	}
	now := h.Now().UTC()
	err = h.Codes.Save(ctx, &domain.Code{
		Hash:        hash,
		ClientID:    client.ID,
		ClientName:  client.Name,
		RedirectURI: req.RedirectURI,
		Challenge:   req.CodeChallenge,
		Resource:    req.Resource,
		Scope:       req.Scope,
		Owner: domain.Owner{
			UserID:        principal.UserID,
			Name:          principal.Name,
			Email:         principal.Email,
			EmailVerified: principal.EmailVerified,
		},
		ExpiresAt: now.Add(domain.CodeTTL),
		CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// ---- token endpoint ----

// ExchangeCode redeems an authorization code for a new grant's tokens
// (RFC 6749 section 4.1.3 with PKCE). The code is used up whatever the
// outcome.
type ExchangeCode struct {
	ClientID     string
	Code         string
	RedirectURI  string
	CodeVerifier string
	Resource     string
}

type ExchangeCodeHandler struct{ base }

func (h ExchangeCodeHandler) Handle(ctx context.Context, cmd ExchangeCode) (TokenSet, error) {
	if cmd.Code == "" || cmd.ClientID == "" {
		return TokenSet{}, domain.ErrInvalidRequest
	}
	code, err := h.Codes.Consume(ctx, opaque.Hash(cmd.Code))
	if err != nil {
		return TokenSet{}, apperrors.NotFoundAs(err, domain.ErrInvalidGrant)
	}
	now := h.Now().UTC()
	switch {
	case code.Expired(now), code.ClientID != cmd.ClientID, code.RedirectURI != cmd.RedirectURI,
		!domain.VerifyPKCE(code.Challenge, cmd.CodeVerifier):
		return TokenSet{}, domain.ErrInvalidGrant
	case cmd.Resource != "" && !domain.SameResource(cmd.Resource, code.Resource):
		return TokenSet{}, domain.ErrInvalidTarget
	}

	s, err := newSecrets()
	if err != nil {
		return TokenSet{}, err
	}
	grant := &domain.Grant{
		ClientID:         code.ClientID,
		ClientName:       code.ClientName,
		Scope:            code.Scope,
		Resource:         code.Resource,
		Owner:            code.Owner,
		AccessHash:       s.accessHash,
		AccessExpiresAt:  now.Add(domain.AccessTokenTTL),
		RefreshHash:      s.refreshHash,
		RefreshExpiresAt: now.Add(domain.RefreshTokenTTL),
		CreatedAt:        now,
		LastUsedAt:       now,
	}
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.makeRoom(ctx, code.Owner.UserID); err != nil {
			return err
		}
		return h.Grants.Create(ctx, grant)
	})
	if err != nil {
		return TokenSet{}, err
	}
	return s.tokenSet(grant.ID, grant.Scope), nil
}

// makeRoom revokes the user's least recently used grants beyond
// domain.MaxGrantsPerUser - 1, so the new one fits.
func (h ExchangeCodeHandler) makeRoom(ctx context.Context, userID string) error {
	grants, err := h.Grants.ListForUser(ctx, userID)
	if err != nil {
		return err
	}
	for len(grants) >= domain.MaxGrantsPerUser {
		oldest := grants[len(grants)-1]
		if err := h.Grants.Revoke(ctx, oldest.ID); err != nil {
			return err
		}
		grants = grants[:len(grants)-1]
	}
	return nil
}

// Refresh exchanges a refresh token for new tokens and rotates it: the old
// one stops working. Presenting a refresh token that was already rotated
// means it was copied, so the whole grant is revoked (OAuth 2.1 section
// 4.3.1).
type Refresh struct {
	ClientID     string
	RefreshToken string
	Resource     string
	Scope        string
}

type RefreshHandler struct{ base }

func (h RefreshHandler) Handle(ctx context.Context, cmd Refresh) (TokenSet, error) {
	grantID, secret, ok := domain.ParseToken(domain.RefreshToken, cmd.RefreshToken)
	if !ok || cmd.ClientID == "" {
		return TokenSet{}, domain.ErrInvalidGrant
	}
	grant, err := h.Grants.Get(ctx, grantID)
	if err != nil {
		return TokenSet{}, invalidGrant(err)
	}
	now := h.Now().UTC()
	switch {
	case grant.ClientID != cmd.ClientID:
		return TokenSet{}, domain.ErrInvalidGrant
	case !domain.SameHash(grant.RefreshHash, opaque.Hash(secret)):
		// An earlier refresh token of this grant: someone kept a copy.
		if err := h.Grants.Revoke(ctx, grant.ID); err != nil {
			return TokenSet{}, err
		}
		return TokenSet{}, domain.ErrInvalidGrant
	case !now.Before(grant.RefreshExpiresAt):
		if err := h.Grants.Revoke(ctx, grant.ID); err != nil {
			return TokenSet{}, err
		}
		return TokenSet{}, domain.ErrInvalidGrant
	case cmd.Resource != "" && !domain.SameResource(cmd.Resource, grant.Resource):
		return TokenSet{}, domain.ErrInvalidTarget
	}
	if cmd.Scope != "" {
		scope, err := domain.ParseScope(cmd.Scope)
		if err != nil || !scopeWithin(scope, grant.Scope) {
			return TokenSet{}, domain.ErrInvalidScope
		}
	}

	s, err := newSecrets()
	if err != nil {
		return TokenSet{}, err
	}
	err = h.Grants.Rotate(ctx, application.Rotation{
		GrantID:          grant.ID,
		RefreshHash:      grant.RefreshHash,
		AccessHash:       s.accessHash,
		AccessExpiresAt:  now.Add(domain.AccessTokenTTL),
		NewRefreshHash:   s.refreshHash,
		RefreshExpiresAt: now.Add(domain.RefreshTokenTTL),
		UsedAt:           now,
	})
	if err != nil {
		return TokenSet{}, invalidGrant(err) // a concurrent refresh won
	}
	return s.tokenSet(grant.ID, grant.Scope), nil
}

// scopeWithin reports whether every scope in requested is in granted.
func scopeWithin(requested, granted string) bool {
	have := strings.Fields(granted)
	for _, s := range strings.Fields(requested) {
		if s != domain.OfflineAccess && !slices.Contains(have, s) {
			return false
		}
	}
	return true
}

func invalidGrant(err error) error {
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrInvalidID) {
		return domain.ErrInvalidGrant
	}
	return err
}

// RevokeToken revokes the grant an access or refresh token belongs to
// (RFC 7009). Unknown tokens, and tokens of another client, are ignored:
// the endpoint answers the same either way.
type RevokeToken struct {
	ClientID string
	Token    string
}

type RevokeTokenHandler struct{ base }

func (h RevokeTokenHandler) Handle(ctx context.Context, cmd RevokeToken) error {
	grantID, secret, ok := domain.ParseToken(domain.RefreshToken, cmd.Token)
	kind := domain.RefreshToken
	if !ok {
		grantID, secret, ok = domain.ParseToken(domain.AccessToken, cmd.Token)
		kind = domain.AccessToken
	}
	if !ok {
		return nil
	}
	grant, err := h.Grants.Get(ctx, grantID)
	if err != nil {
		if invalidGrant(err) == domain.ErrInvalidGrant {
			return nil
		}
		return err
	}
	stored := grant.RefreshHash
	if kind == domain.AccessToken {
		stored = grant.AccessHash
	}
	if grant.ClientID != cmd.ClientID || !domain.SameHash(stored, opaque.Hash(secret)) {
		return nil
	}
	return h.Grants.Revoke(ctx, grant.ID)
}

// ---- connections (the user's side) ----

// RevokeConnection disconnects one of the user's connected apps: its tokens
// stop working at once.
type RevokeConnection struct {
	UserID string
	ID     string
}

type RevokeConnectionHandler struct{ base }

func (h RevokeConnectionHandler) Handle(ctx context.Context, cmd RevokeConnection) error {
	return apperrors.UnresolvedAs(h.Grants.Delete(ctx, cmd.UserID, cmd.ID), domain.ErrConnectionNotFound)
}

// RevokeUserConnections disconnects every app of a user, and drops their
// pending authorization codes: on log-out-everywhere and account deletion.
type RevokeUserConnections struct {
	UserID string
}

type RevokeUserConnectionsHandler struct{ base }

func (h RevokeUserConnectionsHandler) Handle(ctx context.Context, cmd RevokeUserConnections) error {
	if err := h.Codes.DeleteForUser(ctx, cmd.UserID); err != nil {
		return err
	}
	return h.Grants.DeleteForUser(ctx, cmd.UserID)
}
