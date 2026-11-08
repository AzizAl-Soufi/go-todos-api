package ids

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

func TestObjectID(t *testing.T) {
	want := bson.NewObjectID()
	if got, err := ObjectID(want.Hex()); err != nil || got != want {
		t.Fatalf("ObjectID(valid) = %v, %v", got, err)
	}

	for _, bad := range []string{"", "xyz", uuid.NewString()} {
		got, err := ObjectID(bad)
		if !errors.Is(err, apperrors.ErrInvalidID) || got != bson.NilObjectID {
			t.Errorf("ObjectID(%q) = %v, %v; want NilObjectID, ErrInvalidID", bad, got, err)
		}
	}
}

func TestUUID(t *testing.T) {
	want := uuid.New()
	if got, err := UUID(want.String()); err != nil || got != want {
		t.Fatalf("UUID(valid) = %v, %v", got, err)
	}

	for _, bad := range []string{"", "xyz", bson.NewObjectID().Hex()} {
		got, err := UUID(bad)
		if !errors.Is(err, apperrors.ErrInvalidID) || got != uuid.Nil {
			t.Errorf("UUID(%q) = %v, %v; want uuid.Nil, ErrInvalidID", bad, got, err)
		}
	}
}

func TestPairs(t *testing.T) {
	a, b := bson.NewObjectID(), bson.NewObjectID()
	gotA, gotB, err := ObjectIDPair(a.Hex(), b.Hex())
	if err != nil || gotA != a || gotB != b {
		t.Fatalf("ObjectIDPair = %v, %v, %v", gotA, gotB, err)
	}

	u, v := uuid.New(), uuid.New()
	gotU, gotV, err := UUIDPair(u.String(), v.String())
	if err != nil || gotU != u || gotV != v {
		t.Fatalf("UUIDPair = %v, %v, %v", gotU, gotV, err)
	}

	tests := map[string]func() error{
		"ObjectIDPair bad id":    func() error { _, _, err := ObjectIDPair("bad", a.Hex()); return err },
		"ObjectIDPair bad owner": func() error { _, _, err := ObjectIDPair(a.Hex(), "bad"); return err },
		"UUIDPair bad id":        func() error { _, _, err := UUIDPair("bad", u.String()); return err },
		"UUIDPair bad owner":     func() error { _, _, err := UUIDPair(u.String(), "bad"); return err },
	}
	for name, call := range tests {
		if err := call(); !errors.Is(err, apperrors.ErrInvalidID) {
			t.Errorf("%s: err = %v, want ErrInvalidID", name, err)
		}
	}
}
