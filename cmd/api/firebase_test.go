package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
)

// fakeFirebase plays Firebase Authentication for the end-to-end tests: the
// client side (signing in, exchanging a custom token) and the Admin SDK the
// API uses (verifying ID tokens, minting custom tokens, deleting users).
// The real adapter is covered in identity/adapters/firebase.
type fakeFirebase struct {
	mu      sync.Mutex
	tokens  map[string]application.IDClaims // ID token -> claims
	custom  map[string]string               // custom token -> UID
	deleted []string
}

func newFakeFirebase() *fakeFirebase {
	return &fakeFirebase{tokens: map[string]application.IDClaims{}, custom: map[string]string{}}
}

func uidOf(email string) string { return "uid-" + strings.ToLower(email) }

// signIn is a client signing in to Firebase; it returns the ID token.
func (f *fakeFirebase) signIn(c application.IDClaims) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.UID == "" {
		c.UID = uidOf(c.Email)
	}
	if c.Provider == "" {
		c.Provider = "password"
	}
	if c.AuthTime.IsZero() {
		c.AuthTime = time.Now().Truncate(time.Second)
	}
	tok := fmt.Sprintf("firebase-id-token-%d", len(f.tokens))
	f.tokens[tok] = c
	return tok
}

// exchange is signInWithCustomToken: a custom token becomes an ID token.
func (f *fakeFirebase) exchange(customToken string) (string, bool) {
	f.mu.Lock()
	uid, ok := f.custom[customToken]
	f.mu.Unlock()
	if !ok {
		return "", false
	}
	return f.signIn(application.IDClaims{UID: uid, Provider: "custom"}), true
}

func (f *fakeFirebase) VerifyIDToken(_ context.Context, tok string) (application.IDClaims, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.tokens[tok]
	if !ok {
		return application.IDClaims{}, domain.ErrInvalidIDToken
	}
	return c, nil
}

func (f *fakeFirebase) CustomToken(_ context.Context, uid string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tok := fmt.Sprintf("custom-token-%d", len(f.custom))
	f.custom[tok] = uid
	return tok, nil
}

func (f *fakeFirebase) DeleteUser(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, uid)
	return nil
}
