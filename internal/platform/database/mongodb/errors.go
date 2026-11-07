package mongodb

import (
	"errors"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// MapNotFound translates mongo.ErrNoDocuments into apperrors.ErrNotFound and
// returns any other error (including nil) unchanged.
func MapNotFound(err error) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return apperrors.ErrNotFound
	}
	return err
}

// RequireDeleted reports apperrors.ErrNotFound when a delete matched nothing.
func RequireDeleted(result *mongo.DeleteResult, err error) error {
	if err != nil {
		return MapNotFound(err)
	}
	if result.DeletedCount == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}
