package persistence_test

// IDs in the wrong format are rejected with ErrInvalidID before the stores
// touch the (nil) connection. Behaviour against real databases is covered by
// the storetest contract in mongostore and pgstore.

import (
	"context"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/mongostore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/testutil"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func invalidIDCases(users application.UserRepository, sessions application.SessionRepository, tokens application.OneTimeTokenRepository) map[string]func() error {
	ctx := context.Background()
	now := time.Now()
	return map[string]func() error{
		"FindByID":              func() error { _, err := users.FindByID(ctx, "bad"); return err },
		"MarkEmailVerified":     func() error { return users.MarkEmailVerified(ctx, "bad", now) },
		"IncrementTokenVersion": func() error { return users.IncrementTokenVersion(ctx, "bad") },
		"SetPasswordHash":       func() error { return users.SetPasswordHash(ctx, "bad", "x") },
		"Delete user":           func() error { return users.Delete(ctx, "bad") },
		"Create session":        func() error { return sessions.Create(ctx, &domain.Session{UserID: "bad"}) },
		"Revoke":                func() error { _, err := sessions.Revoke(ctx, "bad", now); return err },
		"RevokeAllForUser":      func() error { return sessions.RevokeAllForUser(ctx, "bad", now) },
		"DeleteAllForUser":      func() error { return sessions.DeleteAllForUser(ctx, "bad") },
		"Save token":            func() error { return tokens.Save(ctx, &domain.OneTimeToken{UserID: "bad"}) },
		"DeleteForUser":         func() error { return tokens.DeleteForUser(ctx, "bad") },
	}
}

func TestMongoStores_RejectInvalidIDs(t *testing.T) {
	var u mongostore.Users
	var s mongostore.Sessions
	var v mongostore.OneTimeTokens
	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, invalidIDCases(&u, &s, &v))
	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, map[string]func() error{
		"FindByID with a UUID": func() error { _, err := u.FindByID(context.Background(), uuid.NewString()); return err },
	})
}

func TestPostgresStores_RejectInvalidIDs(t *testing.T) {
	cases := invalidIDCases(pgstore.NewUsers(nil), pgstore.NewSessions(nil), pgstore.NewPasswordResets(nil))
	cases["RevokeFamily"] = func() error { return pgstore.NewSessions(nil).RevokeFamily(context.Background(), "bad", time.Now()) }
	cases["FindByID with an ObjectID"] = func() error {
		_, err := pgstore.NewUsers(nil).FindByID(context.Background(), bson.NewObjectID().Hex())
		return err
	}
	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, cases)
}
