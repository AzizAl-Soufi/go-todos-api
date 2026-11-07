package postgres

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
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

func TestExecutorWithoutTransactionUsesPool(t *testing.T) {
	if got := Executor(context.Background(), nil); got == nil {
		t.Fatal("Executor must return the pool (here a typed nil) when ctx carries no transaction")
	}
}

func TestMigrationsAreEmbedded(t *testing.T) {
	entries, err := fs.ReadDir(Migrations(), ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{"00001_create_users.sql", "00002_create_todos.sql", "00003_identity_auth.sql", "00004_password_resets.sql", "00005_todos_user_index.sql", "00006_projects_collections.sql",
		"00007_teams.sql", "00008_audit_events.sql", "00009_team_workspaces.sql", "00010_passkeys.sql"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("embedded migrations = %v, want %v", names, want)
	}
}
