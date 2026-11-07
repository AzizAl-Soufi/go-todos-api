package postgres

import (
	"errors"
	"testing"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/jackc/pgx/v5"
)

func TestMapErr(t *testing.T) {
	if err := MapErr("get todo", nil); err != nil {
		t.Errorf("nil -> %v, want nil", err)
	}
	if err := MapErr("get todo", pgx.ErrNoRows); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("ErrNoRows -> %v, want ErrNotFound", err)
	}

	cause := errors.New("connection reset")
	err := MapErr("get todo", cause)
	if !errors.Is(err, cause) || err.Error() != "get todo: connection reset" {
		t.Errorf("other error -> %v, want wrapped with op", err)
	}
}

func TestRequireAffected(t *testing.T) {
	if err := RequireAffected("delete", 1, nil); err != nil {
		t.Errorf("1 row -> %v, want nil", err)
	}
	if err := RequireAffected("delete", 0, nil); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("0 rows -> %v, want ErrNotFound", err)
	}
	cause := errors.New("boom")
	if err := RequireAffected("delete", 0, cause); !errors.Is(err, cause) {
		t.Errorf("error -> %v, want wrapped cause", err)
	}
}
