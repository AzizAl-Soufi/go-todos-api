package postgres

import (
	"errors"
	"fmt"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MapErr translates pgx.ErrNoRows into apperrors.ErrNotFound and wraps any
// other error with the operation that failed. A nil error stays nil.
func MapErr(op string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return apperrors.ErrNotFound
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}

// RequireAffected reports apperrors.ErrNotFound when a statement touched no rows.
func RequireAffected(op string, rows int64, err error) error {
	if err != nil {
		return MapErr(op, err)
	}
	if rows == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

// IsUniqueViolation reports a unique-constraint violation (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
