// Package security holds the identity module's crypto adapters.
package security

import (
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"golang.org/x/crypto/bcrypt"
)

// DefaultBcryptCost balances login latency (~250ms) against brute-force cost.
const DefaultBcryptCost = 12

type BcryptHasher struct {
	cost int
}

var _ application.PasswordHasher = BcryptHasher{}

// NewBcryptHasher uses cost, or DefaultBcryptCost when cost is 0.
// Tests may pass bcrypt.MinCost to stay fast.
func NewBcryptHasher(cost int) BcryptHasher {
	if cost == 0 {
		cost = DefaultBcryptCost
	}
	return BcryptHasher{cost: cost}
}

func (h BcryptHasher) Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	return string(b), err
}

func (h BcryptHasher) Compare(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
