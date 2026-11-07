// Package opaque makes the random single-use tokens sent by email (refresh
// tokens, verification, password reset and invitation links). Only the hash
// is stored, so a database leak does not expose usable tokens.
package opaque

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// New returns a random URL-safe token and its Hash.
func New() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", apperrors.InternalWrap("RANDOM_FAILED", "failed to generate token", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, Hash(token), nil
}

// Hash is the SHA-256 hex digest of a token, the form that is stored.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
