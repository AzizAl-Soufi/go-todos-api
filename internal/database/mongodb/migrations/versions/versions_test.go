package versions_test

import (
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/mongodb/migrations"
	_ "github.com/AzizAl-Soufi/go-todos-api/internal/database/mongodb/migrations/versions"
)

func TestMigrationsAreRegistered(t *testing.T) {
	all := migrations.GetMigrations()
	if len(all) == 0 {
		t.Fatal("expected at least one registered migration")
	}

	first := all[0]
	if first.Version != 1 || first.Name != "initialize_schema" {
		t.Fatalf("first migration = %d %q, want 1 \"initialize_schema\"", first.Version, first.Name)
	}

	// Versions must be contiguous so the runner never skips a gap.
	for i, m := range all {
		if m.Version != i+1 {
			t.Fatalf("migration #%d has version %d, want %d (versions must be contiguous)", i, m.Version, i+1)
		}
		if m.Name == "" || m.Migrate == nil || m.Rollback == nil {
			t.Fatalf("migration %d is incomplete: %+v", m.Version, m)
		}
	}

	if got := migrations.GetLatestVersion(); got != len(all) {
		t.Fatalf("GetLatestVersion() = %d, want %d", got, len(all))
	}
}
