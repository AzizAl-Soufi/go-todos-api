package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNew_InvalidDSN(t *testing.T) {
	db, err := New(context.Background(), "postgres://user:pass@host:notaport/db")
	if err == nil || !strings.Contains(err.Error(), "failed to parse postgres dsn") {
		t.Fatalf("err = %v, want parse error", err)
	}
	if db != nil {
		t.Fatal("expected nil client on error")
	}
}

func TestNew_UnreachableServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := New(ctx, "postgres://user:pass@127.0.0.1:1/db?connect_timeout=1")
	if err == nil || !strings.Contains(err.Error(), "failed to ping postgres") {
		t.Fatalf("err = %v, want ping error", err)
	}
	if db != nil {
		t.Fatal("expected nil client on error")
	}
}

func TestZeroValueClient(t *testing.T) {
	var p Postgres

	if err := p.Ping(context.Background()); err == nil || err.Error() != "pool not connected" {
		t.Fatalf("Ping err = %v, want \"pool not connected\"", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close on unconnected client = %v, want nil", err)
	}
	if p.Pool() != nil {
		t.Fatal("Pool() should be nil before connecting")
	}
}
