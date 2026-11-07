package middleware

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/shared/httpx"
	"github.com/golang-jwt/jwt/v5"
	"github.com/golang-jwt/jwt/v5/request"
)

type Authorization struct {
	ID    string `json:"id" bson:"id"`
	Name  string `json:"name" bson:"name"`
	Email string `json:"email" bson:"email"`
}

func NewAuthorization(id string, name, email string) *Authorization {
	return &Authorization{
		ID:    id,
		Name:  name,
		Email: email,
	}
}

type contextKey string

const authContextKey contextKey = "authorization"

const (
	TypeAuthToken    string = "access"
	TypeRefreshToken string = "refresh"
)

type JWTCustomClaims struct {
	jwt.RegisteredClaims
	TokenType    string         `json:"token_type"`
	ID           string         `json:"id"`
	CustomerInfo *Authorization `json:"customer_info"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type JWTMiddleware struct {
	verifyKey *rsa.PublicKey
	signKey   *rsa.PrivateKey
}

func NewJWTMiddleware(cfg *config.JWTConfig) (*JWTMiddleware, error) {
	m := &JWTMiddleware{}

	signBytes, err := os.ReadFile(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("could not read private key: %w", err)
	}
	m.signKey, err = jwt.ParseRSAPrivateKeyFromPEM(signBytes)
	if err != nil {
		return nil, fmt.Errorf("could not parse private key: %w", err)
	}

	verifyBytes, err := os.ReadFile(cfg.PublicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("could not read public key: %w", err)
	}
	m.verifyKey, err = jwt.ParseRSAPublicKeyFromPEM(verifyBytes)
	if err != nil {
		return nil, fmt.Errorf("could not parse public key: %w", err)
	}

	return m, nil
}

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

func (m *JWTMiddleware) GenerateTokenPair(user *Authorization) (*TokenPair, error) {
	access, err := m.signToken(user, TypeAuthToken, accessTokenTTL)
	if err != nil {
		return nil, err
	}
	refresh, err := m.signToken(user, TypeRefreshToken, refreshTokenTTL)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

func (m *JWTMiddleware) signToken(user *Authorization, tokenType string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := &JWTCustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		TokenType:    tokenType,
		ID:           user.ID,
		CustomerInfo: user,
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(m.signKey)
}

// newParser only accepts RS256, so a token can never pick its own algorithm.
func newParser() *jwt.Parser {
	return jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}))
}

func (m *JWTMiddleware) keyFunc(*jwt.Token) (any, error) {
	return m.verifyKey, nil
}

func (m *JWTMiddleware) ValidateRefreshToken(tokenString string) (*JWTCustomClaims, error) {
	claims := &JWTCustomClaims{}
	token, err := newParser().ParseWithClaims(tokenString, claims, m.keyFunc)

	if err != nil || !token.Valid {
		return nil, ErrExpiredRefreshToken
	}

	if claims.TokenType != TypeRefreshToken {
		return nil, ErrInvalidRefreshTokenType
	}

	return claims, nil
}

func (m *JWTMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := &JWTCustomClaims{}

		token, err := request.ParseFromRequest(
			r,
			request.AuthorizationHeaderExtractor,
			m.keyFunc,
			request.WithClaims(claims),
			request.WithParser(newParser()),
		)

		if err != nil {
			httpx.RespondErr(w, r, accessTokenError(err))
			return
		}

		if token == nil || !token.Valid {
			httpx.RespondErr(w, r, ErrInvalidAccessToken)
			return
		}

		if claims.TokenType != TypeAuthToken || claims.CustomerInfo == nil {
			httpx.RespondErr(w, r, ErrInvalidAccessTokenType)
			return
		}

		ctx := context.WithValue(r.Context(), authContextKey, claims.CustomerInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// accessTokenError maps a token parsing failure to the error reported to the client.
func accessTokenError(err error) error {
	switch {
	case errors.Is(err, request.ErrNoTokenInRequest):
		return ErrMissingAuthHeader
	case errors.Is(err, jwt.ErrTokenExpired):
		return ErrExpiredAccessToken
	case errors.Is(err, jwt.ErrTokenMalformed):
		return ErrMalformedAccessToken
	default: // bad signature, disallowed algorithm, invalid claims, ...
		return ErrInvalidAccessToken
	}
}

// AuthorizationFrom returns the authenticated caller stored by RequireAuth, or
// ErrUnauthorizedContext when the request did not pass through it.
func AuthorizationFrom(ctx context.Context) (*Authorization, error) {
	auth, ok := GetAuthorization(ctx)
	if !ok {
		return nil, ErrUnauthorizedContext
	}
	return auth, nil
}

func GetAuthorization(ctx context.Context) (*Authorization, bool) {
	auth, ok := ctx.Value(authContextKey).(*Authorization)
	return auth, ok && auth != nil
}
