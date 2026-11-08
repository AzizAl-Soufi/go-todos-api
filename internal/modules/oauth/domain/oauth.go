// Package domain holds the oauth module's entities and rules: the OAuth 2.1
// clients MCP hosts register as, authorization codes, and the grants that
// carry access and refresh tokens. No HTTP, storage or Firebase here.
package domain

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

const (
	// Scope grants the MCP tools: the caller's todos, projects, collections
	// and teams. It is the only scope.
	Scope = "todos"
	// OfflineAccess is accepted (some clients ask for it to get a refresh
	// token); every grant has one anyway.
	OfflineAccess = "offline_access"

	CodeTTL         = 5 * time.Minute
	AccessTokenTTL  = time.Hour
	RefreshTokenTTL = 30 * 24 * time.Hour

	MaxRedirectURIs     = 10
	MaxRedirectURILen   = 2000
	MaxClientNameLength = 100
	// MaxGrantsPerUser bounds connected apps per account; a new one past it
	// replaces the least recently used.
	MaxGrantsPerUser = 25
)

var ErrConnectionNotFound = apperrors.NotFound("CONNECTION_NOT_FOUND", "connection not found")

// Error is an OAuth error (RFC 6749 section 5.2, RFC 7591, RFC 8707): Code
// is the registered error code clients act on.
type Error struct {
	Code        string
	Description string
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

func oauthErr(code, description string) *Error { return &Error{Code: code, Description: description} }

var (
	ErrInvalidRequest          = oauthErr("invalid_request", "the request is missing a parameter or is malformed")
	ErrInvalidClient           = oauthErr("invalid_client", "unknown client")
	ErrInvalidGrant            = oauthErr("invalid_grant", "the code or refresh token is invalid, expired, or was issued to another client")
	ErrUnsupportedGrantType    = oauthErr("unsupported_grant_type", "only authorization_code and refresh_token are supported")
	ErrUnsupportedResponseType = oauthErr("unsupported_response_type", "only response_type=code is supported")
	ErrInvalidScope            = oauthErr("invalid_scope", "the only scopes are todos and offline_access")
	ErrInvalidTarget           = oauthErr("invalid_target", "resource must be this server's MCP endpoint")
	ErrInvalidPKCE             = oauthErr("invalid_request", "PKCE with code_challenge_method=S256 is required")
	ErrAccessDenied            = oauthErr("access_denied", "the user denied the request")
	ErrInvalidRedirectURI      = oauthErr("invalid_redirect_uri", "redirect URIs must be https, or http on a loopback address, without a fragment")
	ErrInvalidClientMetadata   = oauthErr("invalid_client_metadata", "the client metadata is invalid")
)

// ClientKind says where a client's metadata comes from.
type ClientKind string

const (
	// Registered clients registered through Dynamic Client Registration
	// (RFC 7591); their ID is assigned by the server.
	Registered ClientKind = "registered"
	// MetadataDocument clients are identified by the HTTPS URL of their
	// Client ID Metadata Document, which is also their client ID.
	MetadataDocument ClientKind = "metadata_document"
)

// Client is an OAuth client: an MCP host such as Claude. All clients are
// public (no secret): they prove possession of the code with PKCE.
type Client struct {
	ID           string
	Name         string
	RedirectURIs []string
	Kind         ClientKind
	CreatedAt    time.Time
}

// NewClient validates client metadata, from a registration or a metadata
// document. The ID is assigned by the store (registered) or is the
// document's URL.
func NewClient(name string, redirectURIs []string, kind ClientKind, now time.Time) (*Client, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "MCP client"
	}
	if utf8.RuneCountInString(name) > MaxClientNameLength {
		return nil, ErrInvalidClientMetadata
	}
	if len(redirectURIs) == 0 || len(redirectURIs) > MaxRedirectURIs {
		return nil, ErrInvalidRedirectURI
	}
	for _, u := range redirectURIs {
		if err := ValidateRedirectURI(u); err != nil {
			return nil, err
		}
	}
	return &Client{Name: name, RedirectURIs: slices.Clone(redirectURIs), Kind: kind, CreatedAt: now.UTC()}, nil
}

// ValidateRedirectURI accepts absolute https URIs, and http ones on a
// loopback host (native apps, RFC 8252), never with a fragment.
func ValidateRedirectURI(raw string) error {
	if raw == "" || len(raw) > MaxRedirectURILen {
		return ErrInvalidRedirectURI
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" || u.User != nil {
		return ErrInvalidRedirectURI
	}
	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	}
	return ErrInvalidRedirectURI
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// AllowsRedirect reports whether uri is one of the client's redirect URIs.
// The match is exact, except that a loopback http URI matches on any port:
// native clients (Claude Code) listen on a port chosen per sign-in (RFC 8252
// section 7.3).
func (c *Client) AllowsRedirect(uri string) bool {
	for _, registered := range c.RedirectURIs {
		if registered == uri || sameLoopbackIgnoringPort(registered, uri) {
			return true
		}
	}
	return false
}

func sameLoopbackIgnoringPort(registered, requested string) bool {
	r, err1 := url.Parse(registered)
	q, err2 := url.Parse(requested)
	if err1 != nil || err2 != nil || r.Scheme != "http" || q.Scheme != "http" {
		return false
	}
	return isLoopback(r.Hostname()) && r.Hostname() == q.Hostname() &&
		r.Path == q.Path && r.RawQuery == q.RawQuery && q.Fragment == "" && q.User == nil
}

// OnlyLoopback reports whether every redirect goes to the user's own
// machine: any local process can claim such a client, so the consent page
// warns about it.
func (c *Client) OnlyLoopback() bool {
	for _, raw := range c.RedirectURIs {
		u, err := url.Parse(raw)
		if err != nil || !isLoopback(u.Hostname()) {
			return false
		}
	}
	return true
}

// ParseScope checks a space-separated scope list; empty means Scope. The
// result is normalized: Scope, plus OfflineAccess if asked for.
func ParseScope(raw string) (string, error) {
	offline := false
	for _, s := range strings.Fields(raw) {
		switch s {
		case Scope:
		case OfflineAccess:
			offline = true
		default:
			return "", ErrInvalidScope
		}
	}
	if offline {
		return Scope + " " + OfflineAccess, nil
	}
	return Scope, nil
}

// ValidateCodeChallenge checks an S256 PKCE challenge: the base64url
// SHA-256 of a verifier, 43 characters.
func ValidateCodeChallenge(challenge, method string) error {
	if method != "S256" || len(challenge) != 43 || !isBase64URL(challenge) {
		return ErrInvalidPKCE
	}
	return nil
}

// VerifyPKCE checks a code verifier (RFC 7636: 43 to 128 unreserved
// characters) against the challenge, in constant time.
func VerifyPKCE(challenge, verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 || strings.ContainsFunc(verifier, notUnreserved) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

func notUnreserved(r rune) bool {
	return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r))
}

func isBase64URL(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	})
}

// Owner is the account an authorization was granted by, as it was when the
// user signed in to approve it.
type Owner struct {
	UserID        string
	Name          string
	Email         string
	EmailVerified bool
}

// Code is a pending authorization code: single use, short-lived, bound to
// the client, redirect URI and PKCE challenge it was issued for. Only the
// hash of the code is stored.
type Code struct {
	Hash        string
	ClientID    string
	ClientName  string
	RedirectURI string
	Challenge   string
	Resource    string
	Scope       string
	Owner       Owner
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

func (c *Code) Expired(now time.Time) bool { return !now.Before(c.ExpiresAt) }

// Grant is a user's authorization of a client: a connected app. It holds
// the hashes of the current access and refresh token; refreshing replaces
// both (rotation), so an old refresh token presented again means it leaked
// and the grant is revoked.
type Grant struct {
	ID               string
	ClientID         string
	ClientName       string
	Scope            string
	Resource         string
	Owner            Owner
	AccessHash       string
	AccessExpiresAt  time.Time
	RefreshHash      string
	RefreshExpiresAt time.Time
	CreatedAt        time.Time
	// LastUsedAt is the last token issue (approval or refresh).
	LastUsedAt time.Time
}

// TokenKind tells access and refresh tokens apart.
type TokenKind string

const (
	AccessToken  TokenKind = "mcpa"
	RefreshToken TokenKind = "mcpr"
)

// FormatToken builds a token: kind, grant ID and a random secret, joined by
// dots (the secret is base64url, which has none). Only the secret's hash
// is stored; the grant ID finds the grant without an index on hashes.
func FormatToken(kind TokenKind, grantID, secret string) string {
	return string(kind) + "." + grantID + "." + secret
}

// ParseToken splits a token of kind into its grant ID and secret.
func ParseToken(kind TokenKind, token string) (grantID, secret string, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != string(kind) || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// SameHash compares two token hashes in constant time.
func SameHash(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// AuthorizationRequest is an authorization request as the client sent it
// (RFC 6749 section 4.1.1 with PKCE and RFC 8707 resource indicators).
type AuthorizationRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Resource            string
	Scope               string
}

// Check validates the parts of r that don't depend on the client, against
// this server's resource, and normalizes Scope and Resource (empty means
// this server's). Failures are reported to the client's redirect URI.
func (r AuthorizationRequest) Check(resource string) (AuthorizationRequest, error) {
	if r.ResponseType != "code" {
		return r, ErrUnsupportedResponseType
	}
	if err := ValidateCodeChallenge(r.CodeChallenge, r.CodeChallengeMethod); err != nil {
		return r, err
	}
	scope, err := ParseScope(r.Scope)
	if err != nil {
		return r, err
	}
	r.Scope = scope
	if r.Resource == "" {
		r.Resource = resource
	}
	if !SameResource(r.Resource, resource) {
		return r, ErrInvalidTarget
	}
	return r, nil
}

// SameResource compares resource indicators, tolerating a trailing slash.
func SameResource(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}
