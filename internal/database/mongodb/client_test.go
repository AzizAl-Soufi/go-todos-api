package mongodb

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/config"
)

func TestNew_SelectsDatabaseWithoutDialing(t *testing.T) {
	// mongo.Connect is lazy, so this succeeds without a running server.
	db, err := New(context.Background(), &config.DatabaseConfig{
		DatabaseURI:  "mongodb://127.0.0.1:1",
		DatabaseName: "todos_test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })

	if got := db.Database().Name(); got != "todos_test" {
		t.Fatalf("Database().Name() = %q, want todos_test", got)
	}
}

func TestNew_InvalidURI(t *testing.T) {
	_, err := New(context.Background(), &config.DatabaseConfig{DatabaseURI: "not-a-uri", DatabaseName: "x"})
	if err == nil || !strings.Contains(err.Error(), "failed to connect to mongodb") {
		t.Fatalf("err = %v, want connect error", err)
	}
}

func TestPing_UnreachableServer(t *testing.T) {
	db, err := New(context.Background(), &config.DatabaseConfig{
		DatabaseURI:  "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=200",
		DatabaseName: "x",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.Ping(ctx); err == nil {
		t.Fatal("expected Ping to fail against an unreachable server")
	}
}

func TestZeroValueClient(t *testing.T) {
	var m MongoDB

	if err := m.Ping(context.Background()); err == nil || err.Error() != "client not connected" {
		t.Fatalf("Ping err = %v, want \"client not connected\"", err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatalf("Close on unconnected client = %v, want nil", err)
	}
	if m.Database() != nil {
		t.Fatal("Database() should be nil before connecting")
	}
}
