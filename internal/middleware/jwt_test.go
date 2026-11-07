package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/testutil"
	"github.com/golang-jwt/jwt/v5"
)

func newTestMiddleware(t *testing.T) *JWTMiddleware {
	t.Helper()
	priv, pub := testutil.WriteRSAKeyFiles(t, testutil.NewRSAKey(t))
	m, err := NewJWTMiddleware(&config.JWTConfig{PrivateKeyPath: priv, PublicKeyPath: pub})
	if err != nil {
		t.Fatalf("NewJWTMiddleware: %v", err)
	}
	return m
}

func signClaims(t *testing.T, m *JWTMiddleware, claims *JWTCustomClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(m.signKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

var testUser = NewAuthorization("user-1", "Alice", "alice@example.com")

// hs256Refresh returns an otherwise-valid refresh token signed with HS256.
func hs256Refresh(t *testing.T) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &JWTCustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
		TokenType:        TypeRefreshToken,
		CustomerInfo:     testUser,
	}).SignedString([]byte("guessable-secret"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewAuthorization(t *testing.T) {
	a := NewAuthorization("id", "name", "email")
	if a.ID != "id" || a.Name != "name" || a.Email != "email" {
		t.Fatalf("unexpected authorization: %+v", a)
	}
}

func TestNewJWTMiddleware_Errors(t *testing.T) {
	dir := t.TempDir()
	priv, pub := testutil.WriteRSAKeyFiles(t, testutil.NewRSAKey(t))
	garbage := filepath.Join(dir, "garbage.pem")
	if err := os.WriteFile(garbage, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.pem")

	tests := []struct {
		name    string
		cfg     config.JWTConfig
		wantErr string
	}{
		{"missing private key", config.JWTConfig{PrivateKeyPath: missing, PublicKeyPath: pub}, "could not read private key"},
		{"invalid private key", config.JWTConfig{PrivateKeyPath: garbage, PublicKeyPath: pub}, "could not parse private key"},
		{"missing public key", config.JWTConfig{PrivateKeyPath: priv, PublicKeyPath: missing}, "could not read public key"},
		{"invalid public key", config.JWTConfig{PrivateKeyPath: priv, PublicKeyPath: garbage}, "could not parse public key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewJWTMiddleware(&tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
			if m != nil {
				t.Fatal("expected nil middleware on error")
			}
		})
	}
}

func TestGenerateTokenPair_ClaimsAndExpiry(t *testing.T) {
	m := newTestMiddleware(t)

	pair, err := m.GenerateTokenPair(testUser)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" || pair.AccessToken == pair.RefreshToken {
		t.Fatalf("unexpected token pair: %+v", pair)
	}

	parse := func(s string) *JWTCustomClaims {
		claims := &JWTCustomClaims{}
		if _, err := jwt.ParseWithClaims(s, claims, func(*jwt.Token) (any, error) { return m.verifyKey, nil }); err != nil {
			t.Fatalf("parse token: %v", err)
		}
		return claims
	}

	access := parse(pair.AccessToken)
	if access.TokenType != TypeAuthToken || access.ID != testUser.ID || *access.CustomerInfo != *testUser {
		t.Fatalf("unexpected access claims: %+v", access)
	}
	if ttl := time.Until(access.ExpiresAt.Time); ttl <= 14*time.Minute || ttl > 15*time.Minute {
		t.Fatalf("access token TTL = %v, want ~15m", ttl)
	}

	refresh := parse(pair.RefreshToken)
	if refresh.TokenType != TypeRefreshToken || refresh.ID != testUser.ID {
		t.Fatalf("unexpected refresh claims: %+v", refresh)
	}
	if ttl := time.Until(refresh.ExpiresAt.Time); ttl <= 167*time.Hour || ttl > 168*time.Hour {
		t.Fatalf("refresh token TTL = %v, want ~7d", ttl)
	}
}

func TestValidateRefreshToken(t *testing.T) {
	m := newTestMiddleware(t)
	other := newTestMiddleware(t)

	pair, err := m.GenerateTokenPair(testUser)
	if err != nil {
		t.Fatal(err)
	}
	otherPair, err := other.GenerateTokenPair(testUser)
	if err != nil {
		t.Fatal(err)
	}
	expired := signClaims(t, m, &JWTCustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))},
		TokenType:        TypeRefreshToken,
		CustomerInfo:     testUser,
	})

	t.Run("valid", func(t *testing.T) {
		claims, err := m.ValidateRefreshToken(pair.RefreshToken)
		if err != nil {
			t.Fatalf("ValidateRefreshToken: %v", err)
		}
		if claims.CustomerInfo.ID != testUser.ID {
			t.Fatalf("CustomerInfo.ID = %q, want %q", claims.CustomerInfo.ID, testUser.ID)
		}
	})

	tests := []struct {
		name  string
		token string
		want  error
	}{
		{"access token rejected", pair.AccessToken, ErrInvalidRefreshTokenType},
		{"HS256 algorithm", hs256Refresh(t), ErrExpiredRefreshToken},
		{"expired", expired, ErrExpiredRefreshToken},
		{"wrong signing key", otherPair.RefreshToken, ErrExpiredRefreshToken},
		{"malformed", "not.a.jwt", ErrExpiredRefreshToken},
		{"empty", "", ErrExpiredRefreshToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := m.ValidateRefreshToken(tt.token)
			if err != tt.want {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if claims != nil {
				t.Fatal("expected nil claims on error")
			}
		})
	}
}

func TestRequireAuth_ValidAccessTokenSetsContext(t *testing.T) {
	m := newTestMiddleware(t)
	pair, err := m.GenerateTokenPair(testUser)
	if err != nil {
		t.Fatal(err)
	}

	var got *Authorization
	var ok bool
	handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = GetAuthorization(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body: %s)", rec.Code, rec.Body)
	}
	if !ok || *got != *testUser {
		t.Fatalf("GetAuthorization = %+v, %v; want %+v, true", got, ok, testUser)
	}
}

func TestRequireAuth_Rejections(t *testing.T) {
	m := newTestMiddleware(t)
	other := newTestMiddleware(t)

	pair, err := m.GenerateTokenPair(testUser)
	if err != nil {
		t.Fatal(err)
	}
	otherPair, err := other.GenerateTokenPair(testUser)
	if err != nil {
		t.Fatal(err)
	}
	expired := signClaims(t, m, &JWTCustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))},
		TokenType:        TypeAuthToken,
		CustomerInfo:     testUser,
	})
	noCustomer := signClaims(t, m, &JWTCustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))},
		TokenType:        TypeAuthToken,
	})

	// Tokens using an algorithm other than RS256 must be rejected outright.
	validClaims := &JWTCustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))},
		TokenType:        TypeAuthToken,
		CustomerInfo:     testUser,
	}
	hs256, err := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims).SignedString([]byte("guessable-secret"))
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		header      string
		wantMessage string
	}{
		{"missing header", "", ErrMissingAuthHeader.Message()},
		{"malformed token", "Bearer not-a-jwt", ErrMalformedAccessToken.Message()},
		{"expired token", "Bearer " + expired, ErrExpiredAccessToken.Message()},
		{"wrong signing key", "Bearer " + otherPair.AccessToken, ErrInvalidAccessToken.Message()},
		{"HS256 algorithm", "Bearer " + hs256, ErrInvalidAccessToken.Message()},
		{"none algorithm", "Bearer " + unsigned, ErrInvalidAccessToken.Message()},
		{"refresh token used as access", "Bearer " + pair.RefreshToken, ErrInvalidAccessTokenType.Message()},
		{"missing customer info", "Bearer " + noCustomer, ErrInvalidAccessTokenType.Message()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if called {
				t.Fatal("next handler must not be called")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tt.wantMessage {
				t.Fatalf("error = %q, want %q", body["error"], tt.wantMessage)
			}
		})
	}
}

func TestGetAuthorization(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		if a, ok := GetAuthorization(context.Background()); ok || a != nil {
			t.Fatalf("got %v, %v; want nil, false", a, ok)
		}
	})

	t.Run("typed nil", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), authContextKey, (*Authorization)(nil))
		if _, ok := GetAuthorization(ctx); ok {
			t.Fatal("expected ok=false for a nil *Authorization")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), authContextKey, "not an authorization")
		if _, ok := GetAuthorization(ctx); ok {
			t.Fatal("expected ok=false for a value of the wrong type")
		}
	})

	t.Run("present", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), authContextKey, testUser)
		a, ok := GetAuthorization(ctx)
		if !ok || a != testUser {
			t.Fatalf("got %v, %v; want %v, true", a, ok, testUser)
		}
	})
}
