package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
)

// TestUpgrade_TodosGainStatus checks migration 6 on existing data: the
// completed flag becomes a status (and completed_at), and rolling back
// restores the flag. Needs TEST_POSTGRES_URI.
func TestUpgrade_TodosGainStatus(t *testing.T) {
	pool := dbtest.PostgresEmpty(t)
	ctx := context.Background()
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { db.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, db, postgres.Migrations())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := provider.UpTo(ctx, 5); err != nil {
		t.Fatal(err)
	}
	exec(t, db, `INSERT INTO users (id, name, email) VALUES ('00000000-0000-0000-0000-000000000001', 'Old', 'old@example.com')`)
	exec(t, db, `INSERT INTO todos (user_id, title, completed, created_at) VALUES
		('00000000-0000-0000-0000-000000000001', 'finished', true,  '2026-01-01T00:00:00Z'),
		('00000000-0000-0000-0000-000000000001', 'open',     false, '2026-01-02T00:00:00Z')`)

	if _, err := provider.UpTo(ctx, 6); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT title, status, completed_at IS NOT NULL, updated_at = created_at, priority, tags, project_id IS NULL FROM todos ORDER BY title`)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		title, status                string
		hasCompletedAt, updatedIsNew bool
		priority                     int
		tags                         string
		inbox                        bool
	}
	var got []row
	defer rows.Close()
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.title, &r.status, &r.hasCompletedAt, &r.updatedIsNew, &r.priority, &r.tags, &r.inbox); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []row{
		{"finished", "done", true, true, 0, "{}", true},
		{"open", "pending", false, true, 0, "{}", true},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("after upgrade:\n got  %+v\n want %+v", got, want)
	}

	if _, err := provider.DownTo(ctx, 5); err != nil {
		t.Fatal(err)
	}
	var finished, open bool
	if err := db.QueryRowContext(ctx, `SELECT
		(SELECT completed FROM todos WHERE title = 'finished'),
		(SELECT completed FROM todos WHERE title = 'open')`).Scan(&finished, &open); err != nil {
		t.Fatal(err)
	}
	if !finished || open {
		t.Fatalf("after rollback: finished=%v open=%v", finished, open)
	}
}

func exec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// TestUpgrade_TeamWorkspaces checks migration 9 on existing data: records
// stay personal (no team), and records no longer depend on the users table
// (team records outlive their creator). Rolling back restores the foreign
// keys. Needs TEST_POSTGRES_URI.
func TestUpgrade_TeamWorkspaces(t *testing.T) {
	pool := dbtest.PostgresEmpty(t)
	ctx := context.Background()
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { db.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, db, postgres.Migrations())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := provider.UpTo(ctx, 8); err != nil {
		t.Fatal(err)
	}
	exec(t, db, `INSERT INTO users (id, name, email) VALUES ('00000000-0000-0000-0000-000000000001', 'Old', 'old@example.com')`)
	exec(t, db, `INSERT INTO todos (user_id, title) VALUES ('00000000-0000-0000-0000-000000000001', 'kept')`)

	if _, err := provider.UpTo(ctx, 9); err != nil {
		t.Fatal(err)
	}
	var personal bool
	if err := db.QueryRowContext(ctx, `SELECT team_id IS NULL FROM todos WHERE title = 'kept'`).Scan(&personal); err != nil || !personal {
		t.Fatalf("existing todo must stay personal: %v %v", personal, err)
	}
	// A team todo whose creator has no account row is now allowed.
	exec(t, db, `INSERT INTO todos (user_id, team_id, title) VALUES (gen_random_uuid(), gen_random_uuid(), 'team')`)

	if _, err := provider.DownTo(ctx, 8); err != nil {
		t.Fatal(err)
	}
	var titles []string
	rows, err := db.QueryContext(ctx, `SELECT title FROM todos ORDER BY title`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		titles = append(titles, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(titles) != 1 || titles[0] != "kept" {
		t.Fatalf("after rollback: %v (team records are removed)", titles)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO todos (user_id, title) VALUES (gen_random_uuid(), 'orphan')`); err == nil {
		t.Fatal("rollback must restore the users foreign key")
	}
}
