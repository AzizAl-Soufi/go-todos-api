package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/testutil"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func TestBcryptHasher(t *testing.T) {
	h := NewBcryptHasher(bcrypt.MinCost)
	hash, err := h.Hash("password1")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "password1" || !strings.HasPrefix(hash, "$2") {
		t.Fatalf("not a bcrypt hash: %q", hash)
	}
	if err := h.Compare(hash, "password1"); err != nil {
		t.Fatalf("matching password rejected: %v", err)
	}
	if err := h.Compare(hash, "password2"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if NewBcryptHasher(0).cost != DefaultBcryptCost {
		t.Fatal("zero cost must fall back to the default")
	}
}

var user = &domain.User{ID: "u1", Name: "Ann", Email: "ann@example.com", TokenVersion: 3}

func TestJWT_IssueAndParse(t *testing.T) {
	j := NewJWT(testutil.NewRSAKey(t), time.Minute)
	now := time.Now()

	token, exp, err := j.Issue(user, now)
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(now.Add(time.Minute)) {
		t.Fatalf("expiresAt = %v, want now+1m", exp)
	}
	claims, err := j.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != "u1" || claims.TokenVersion != 3 {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestJWT_Rejections(t *testing.T) {
	key := testutil.NewRSAKey(t)
	j := NewJWT(key, time.Minute)
	other := NewJWT(testutil.NewRSAKey(t), time.Minute)

	sign := func(method jwt.SigningMethod, signKey any, claims accessClaims) string {
		t.Helper()
		s, err := jwt.NewWithClaims(method, claims).SignedString(signKey)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	valid := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u1", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))},
		TokenType:        accessTokenType,
	}
	expired := valid
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	noExpiry := valid
	noExpiry.ExpiresAt = nil
	wrongType := valid
	wrongType.TokenType = "refresh"
	noSubject := valid
	noSubject.Subject = ""
	otherToken, _, _ := other.Issue(user, time.Now())

	cases := map[string]struct {
		token string
		want  error
	}{
		"expired":          {sign(jwt.SigningMethodRS256, key, expired), domain.ErrExpiredAccessToken},
		"malformed":        {"not-a-jwt", domain.ErrMalformedAccessToken},
		"wrong key":        {otherToken, domain.ErrInvalidAccessToken},
		"HS256":            {sign(jwt.SigningMethodHS256, []byte("secret"), valid), domain.ErrInvalidAccessToken},
		"alg none":         {sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, valid), domain.ErrInvalidAccessToken},
		"no expiry":        {sign(jwt.SigningMethodRS256, key, noExpiry), domain.ErrInvalidAccessToken},
		"wrong token type": {sign(jwt.SigningMethodRS256, key, wrongType), domain.ErrInvalidAccessToken},
		"missing subject":  {sign(jwt.SigningMethodRS256, key, noSubject), domain.ErrInvalidAccessToken},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := j.Parse(c.token); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestNewJWTFromFiles(t *testing.T) {
	key := testutil.NewRSAKey(t)
	priv, pub := testutil.WriteRSAKeyFiles(t, key)

	if _, err := NewJWTFromFiles(priv, pub, 0); err != nil {
		t.Fatalf("valid key files: %v", err)
	}

	_, otherPub := testutil.WriteRSAKeyFiles(t, testutil.NewRSAKey(t))
	garbage := filepath.Join(t.TempDir(), "garbage.pem")
	os.WriteFile(garbage, []byte("nope"), 0o600)
	missing := filepath.Join(t.TempDir(), "missing.pem")

	cases := map[string][2]string{
		"mismatched pair":     {priv, otherPub},
		"missing private key": {missing, pub},
		"invalid private key": {garbage, pub},
		"missing public key":  {priv, missing},
		"invalid public key":  {priv, garbage},
	}
	for name, paths := range cases {
		if _, err := NewJWTFromFiles(paths[0], paths[1], 0); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
