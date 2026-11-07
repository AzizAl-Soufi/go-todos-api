package security

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/golang-jwt/jwt/v5"
)

// DefaultAccessTokenTTL keeps access tokens short-lived; refresh tokens renew them.
const DefaultAccessTokenTTL = 15 * time.Minute

const accessTokenType = "access"

// JWT issues and verifies RS256 access tokens.
type JWT struct {
	signKey   *rsa.PrivateKey
	verifyKey *rsa.PublicKey
	ttl       time.Duration
	parser    *jwt.Parser
}

var _ application.AccessTokens = (*JWT)(nil)

type accessClaims struct {
	jwt.RegisteredClaims
	TokenType    string `json:"token_type"`
	TokenVersion int    `json:"ver"`
	Name         string `json:"name,omitempty"`
	Email        string `json:"email,omitempty"`
}

func NewJWT(key *rsa.PrivateKey, ttl time.Duration) *JWT {
	if ttl == 0 {
		ttl = DefaultAccessTokenTTL
	}
	return &JWT{
		signKey:   key,
		verifyKey: &key.PublicKey,
		ttl:       ttl,
		// Only RS256 is accepted, so a token can never pick its own algorithm
		// (no "none", no HS256 signed with the public key).
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
			jwt.WithExpirationRequired(),
		),
	}
}

// NewJWTFromFiles loads a PEM private key and its PEM public key.
func NewJWTFromFiles(privateKeyPath, publicKeyPath string, ttl time.Duration) (*JWT, error) {
	privPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("could not read private key: %w", err)
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privPEM)
	if err != nil {
		return nil, fmt.Errorf("could not parse private key: %w", err)
	}
	pubPEM, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("could not read public key: %w", err)
	}
	pub, err := jwt.ParseRSAPublicKeyFromPEM(pubPEM)
	if err != nil {
		return nil, fmt.Errorf("could not parse public key: %w", err)
	}
	if !pub.Equal(&key.PublicKey) {
		return nil, errors.New("public key does not match private key")
	}
	return NewJWT(key, ttl), nil
}

func (j *JWT) Issue(u *domain.User, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(j.ttl)
	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		TokenType:    accessTokenType,
		TokenVersion: u.TokenVersion,
		Name:         u.Name,
		Email:        u.Email,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(j.signKey)
	return token, expiresAt, err
}

func (j *JWT) Parse(token string) (application.AccessClaims, error) {
	var claims accessClaims
	_, err := j.parser.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return j.verifyKey, nil })
	switch {
	case err == nil:
	case errors.Is(err, jwt.ErrTokenExpired):
		return application.AccessClaims{}, domain.ErrExpiredAccessToken
	case errors.Is(err, jwt.ErrTokenMalformed):
		return application.AccessClaims{}, domain.ErrMalformedAccessToken
	default: // bad signature, disallowed algorithm, invalid claims, ...
		return application.AccessClaims{}, domain.ErrInvalidAccessToken
	}
	if claims.TokenType != accessTokenType || claims.Subject == "" {
		return application.AccessClaims{}, domain.ErrInvalidAccessToken
	}
	return application.AccessClaims{UserID: claims.Subject, TokenVersion: claims.TokenVersion}, nil
}
