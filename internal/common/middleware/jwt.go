package middleware

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/common"
	"github.com/AzizAl-Soufi/go-todos-api/internal/common/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/golang-jwt/jwt/v5/request"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Authorization struct {
	ID    bson.ObjectID `json:"id" bson:"id"`
	Name  string        `json:"name" bson:"name"`
	Email string        `json:"email" bson:"email"`
}

func NewAuthorization(id bson.ObjectID, name, email string) *Authorization {
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
	TokenType    string `json:"token_type"`
	ID           string `json:"id"`
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
	middlware := &JWTMiddleware{}

	signBytes, err := os.ReadFile(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("could not read private key: %w", err)
	}
	middlware.signKey, err = jwt.ParseRSAPrivateKeyFromPEM(signBytes)
	if err != nil {
		return nil, fmt.Errorf("could not parse private key: %w", err)
	}

	verifyBytes, err := os.ReadFile(cfg.PublicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("could not read public key: %w", err)
	}
	middlware.verifyKey, err = jwt.ParseRSAPublicKeyFromPEM(verifyBytes)
	if err != nil {
		return nil, fmt.Errorf("could not parse public key: %w", err)
	}

	return middlware, nil
}

func (middlware *JWTMiddleware) GenerateTokenPair(user *Authorization) (*TokenPair, error) {
	accessTokenClaims := &JWTCustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
		TokenType:    TypeAuthToken,
		ID:           user.ID.Hex(),
		CustomerInfo: user,
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodRS256, accessTokenClaims)
	accessTokenString, err := accessToken.SignedString(middlware.signKey)
	if err != nil {
		return nil, err
	}

	refreshTokenClaims := &JWTCustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
		},
		TokenType:    TypeRefreshToken,
		ID:           user.ID.Hex(),
		CustomerInfo: user,
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodRS256, refreshTokenClaims)
	refreshTokenString, err := refreshToken.SignedString(middlware.signKey)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
	}, nil
}

func (middlware *JWTMiddleware) ValidateRefreshToken(tokenString string) (*JWTCustomClaims, error) {
	claims := &JWTCustomClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return middlware.verifyKey, nil
	})

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
			func(token *jwt.Token) (any, error) {
				return m.verifyKey, nil
			},
			request.WithClaims(claims),
		)

		if err != nil {
			log.Printf("err : %v", err)
			message := ErrMissingAuthHeader.Error()
			switch {
			case errors.Is(err, jwt.ErrTokenExpired), errors.Is(err, jwt.ErrTokenInvalidClaims):
				message = ErrExpiredAccessToken.Error()
			case errors.Is(err, jwt.ErrTokenInvalidId):
				message = jwt.ErrTokenInvalidId.Error()
			}
			common.RespondError(w, http.StatusUnauthorized, message)
			return
		}

		if token == nil || !token.Valid {
			common.RespondError(w, http.StatusUnauthorized, ErrExpiredAccessToken.Error())
			return
		}

		if claims.TokenType != TypeAuthToken || claims.CustomerInfo == nil {
			common.RespondError(w, http.StatusUnauthorized, ErrInvalidRefreshTokenType.Error())
			return
		}

		ctx := context.WithValue(r.Context(), authContextKey, claims.CustomerInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetAuthorization(ctx context.Context) (*Authorization, bool) {
	auth, ok := ctx.Value(authContextKey).(*Authorization)
	return auth, ok && auth != nil
}
