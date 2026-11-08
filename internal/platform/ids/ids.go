// Package ids parses the string IDs used at the domain boundary into the
// storage-specific ID types, reporting apperrors.ErrInvalidID on bad input.
package ids

import (
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// ObjectID parses a MongoDB ObjectID hex string.
func ObjectID(s string) (bson.ObjectID, error) {
	id, err := bson.ObjectIDFromHex(s)
	if err != nil {
		return bson.NilObjectID, apperrors.ErrInvalidID
	}
	return id, nil
}

// ObjectIDPair parses an entity ID together with its owner's ID.
func ObjectIDPair(id, ownerID string) (bson.ObjectID, bson.ObjectID, error) {
	return pair(ObjectID, id, ownerID)
}

// UUID parses a PostgreSQL UUID string.
func UUID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, apperrors.ErrInvalidID
	}
	return id, nil
}

// UUIDPair parses an entity ID together with its owner's ID.
func UUIDPair(id, ownerID string) (uuid.UUID, uuid.UUID, error) {
	return pair(UUID, id, ownerID)
}

func pair[T any](parse func(string) (T, error), id, ownerID string) (T, T, error) {
	var zero T
	parsedID, err := parse(id)
	if err != nil {
		return zero, zero, err
	}
	parsedOwner, err := parse(ownerID)
	if err != nil {
		return zero, zero, err
	}
	return parsedID, parsedOwner, nil
}
