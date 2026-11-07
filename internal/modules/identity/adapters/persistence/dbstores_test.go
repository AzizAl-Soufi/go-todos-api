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

func invalidIDCases(users application.UserRepository, passkeys application.PasskeyRepository, challenges application.PasskeyChallengeRepository) map[string]func() error {
	ctx := context.Background()
	now := time.Now()
	return map[string]func() error{
		"FindByID":              func() error { _, err := users.FindByID(ctx, "bad"); return err },
		"MarkEmailVerified":     func() error { return users.MarkEmailVerified(ctx, "bad", now) },
		"LinkFirebaseUID":       func() error { return users.LinkFirebaseUID(ctx, "bad", "x") },
		"RevokeSessions":        func() error { return users.RevokeSessions(ctx, "bad", now) },
		"Delete user":           func() error { return users.Delete(ctx, "bad") },
		"Create passkey":        func() error { return passkeys.Create(ctx, &domain.Passkey{UserID: "bad"}) },
		"ListForUser passkeys":  func() error { _, err := passkeys.ListForUser(ctx, "bad"); return err },
		"RecordUse":             func() error { return passkeys.RecordUse(ctx, "bad", nil, now) },
		"Delete passkey":        func() error { return passkeys.Delete(ctx, "bad", "bad") },
		"DeleteAllForUser keys": func() error { return passkeys.DeleteAllForUser(ctx, "bad") },
		"Save challenge":        func() error { return challenges.Save(ctx, &domain.PasskeyChallenge{UserID: "bad"}) },
		"DeleteForUser chall.":  func() error { return challenges.DeleteForUser(ctx, "bad") },
	}
}

func TestMongoStores_RejectInvalidIDs(t *testing.T) {
	var u mongostore.Users
	var p mongostore.Passkeys
	var c mongostore.PasskeyChallenges
	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, invalidIDCases(&u, &p, &c))
	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, map[string]func() error{
		"FindByID with a UUID": func() error { _, err := u.FindByID(context.Background(), uuid.NewString()); return err },
	})
}

func TestPostgresStores_RejectInvalidIDs(t *testing.T) {
	cases := invalidIDCases(pgstore.NewUsers(nil), pgstore.NewPasskeys(nil), pgstore.NewPasskeyChallenges(nil))
	cases["FindByID with an ObjectID"] = func() error {
		_, err := pgstore.NewUsers(nil).FindByID(context.Background(), bson.NewObjectID().Hex())
		return err
	}
	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, cases)
}
