package firebase

import (
	"testing"
	"time"

	"firebase.google.com/go/v4/auth"
)

func TestClaimsOf(t *testing.T) {
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	tok := &auth.Token{
		UID: "uid-1", AuthTime: at.Unix(),
		Firebase: auth.FirebaseInfo{SignInProvider: "password"},
		Claims:   map[string]any{"email": "ann@example.com", "email_verified": true, "name": "Ann"},
	}
	c := claimsOf(tok)
	if c.UID != "uid-1" || c.Email != "ann@example.com" || !c.EmailVerified || c.Name != "Ann" || c.Provider != "password" || !c.AuthTime.Equal(at) {
		t.Fatalf("claims = %+v", c)
	}

	bare := claimsOf(&auth.Token{UID: "uid-2", Claims: map[string]any{"email_verified": "yes"}})
	if bare.Email != "" || bare.EmailVerified || bare.Name != "" {
		t.Fatalf("missing or mistyped claims must be zero: %+v", bare)
	}
}
