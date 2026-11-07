// Package auth carries the authenticated caller between modules.
//
// The identity module implements Authenticator; other modules only depend on
// Principal and the middleware here, never on identity internals.
package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID        string
	Name          string
	Email         string
	EmailVerified bool
}

// Authenticator resolves a bearer access token into a Principal.
type Authenticator interface {
	Authenticate(ctx context.Context, accessToken string) (Principal, error)
}

var (
	ErrMissingToken = apperrors.Unauthorized("MISSING_ACCESS_TOKEN", "authorization header with a bearer token is required")
	ErrNoPrincipal  = apperrors.Unauthorized("UNAUTHORIZED", "request is not authenticated")
	ErrUnverified   = apperrors.Forbidden("EMAIL_NOT_VERIFIED", "email address must be verified first")
)

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok && p.UserID != ""
}

// Middleware authenticates the bearer token and stores the Principal in the
// request context; unauthenticated requests are rejected with 401.
func Middleware(a Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := BearerToken(r)
			if err != nil {
				httpx.RespondErr(w, r, err)
				return
			}
			p, err := a.Authenticate(r.Context(), token)
			if err != nil {
				httpx.RespondErr(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}

// RequireVerifiedEmail rejects callers whose email is not verified (403).
// It must run after Middleware.
func RequireVerifiedEmail(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFrom(r.Context())
		if !ok {
			httpx.RespondErr(w, r, ErrNoPrincipal)
			return
		}
		if !p.EmailVerified {
			httpx.RespondErr(w, r, ErrUnverified)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// BearerToken returns the token of an "Authorization: Bearer <token>"
// header, or ErrMissingToken.
func BearerToken(r *http.Request) (string, error) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", ErrMissingToken
	}
	if token = strings.TrimSpace(token); token == "" {
		return "", ErrMissingToken
	}
	return token, nil
}

// ForUser adapts an endpoint that needs the authenticated caller's user ID;
// without a principal (route not behind Middleware) it answers 401.
func ForUser(fn func(r *http.Request, userID string) (int, any, error)) httpx.Endpoint {
	return func(r *http.Request) (int, any, error) {
		p, ok := PrincipalFrom(r.Context())
		if !ok {
			return 0, nil, ErrNoPrincipal
		}
		return fn(r, p.UserID)
	}
}
