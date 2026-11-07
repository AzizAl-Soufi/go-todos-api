// Package storetest is the contract every audit persistence adapter must
// satisfy; the memory, MongoDB and PostgreSQL stores all run it. Pages are
// checked against domain.Filter and domain.Compare, the shared definition.
package storetest

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

type Backend struct {
	Events application.EventStore
	Tx     tx.Runner
	// AtomicTx is true when Tx really rolls back (not tx.None).
	AtomicTx bool
	// NewID returns a fresh, well-formed ID for this backend (users, teams).
	NewID func() string
}

func Run(t *testing.T, newBackend func(t *testing.T) Backend) {
	t.Run("RoundTrip", func(t *testing.T) { testRoundTrip(t, newBackend(t)) })
	t.Run("Paging", func(t *testing.T) { testPaging(t, newBackend(t)) })
	t.Run("Deletes", func(t *testing.T) { testDeletes(t, newBackend(t)) })
	t.Run("Transactions", func(t *testing.T) {
		b := newBackend(t)
		if !b.AtomicTx {
			t.Skip("this backend runs without transactions")
		}
		testTransactions(t, b)
	})
}

var (
	ctx  = context.Background()
	base = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
)

func appendEvent(t *testing.T, b Backend, e *domain.Event) *domain.Event {
	t.Helper()
	if err := b.Events.Append(ctx, e); err != nil || e.ID == "" {
		t.Fatalf("append: %v, id %q", err, e.ID)
	}
	return e
}

func list(t *testing.T, b Backend, f domain.Filter) []*domain.Event {
	t.Helper()
	got, err := b.Events.List(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func testRoundTrip(t *testing.T, b Backend) {
	team, actor := b.NewID(), b.NewID()
	full := appendEvent(t, b, &domain.Event{
		TeamID: team, ActorID: actor, Action: "member.role_changed", TargetType: "member", TargetID: b.NewID(),
		Details: map[string]string{"from": "viewer", "to": "admin", "note": "ünïcode"}, IP: "203.0.113.7",
		UserAgent: "curl/8", CreatedAt: base,
	})
	user := b.NewID()
	bare := appendEvent(t, b, &domain.Event{UserID: user, Action: "session.login_failed", Details: map[string]string{}, CreatedAt: base})

	got := list(t, b, domain.Filter{TeamID: team, Limit: 10})
	if len(got) != 1 {
		t.Fatalf("team log has %d events", len(got))
	}
	g := got[0]
	if g.ID != full.ID || g.TeamID != team || g.UserID != "" || g.ActorID != actor || g.Action != full.Action ||
		g.TargetType != "member" || g.TargetID != full.TargetID || !maps.Equal(g.Details, full.Details) ||
		g.IP != full.IP || g.UserAgent != full.UserAgent || !g.CreatedAt.Equal(base) {
		t.Fatalf("round trip:\n got  %+v\n want %+v", g, full)
	}

	got = list(t, b, domain.Filter{UserID: user, Limit: 10})
	if len(got) != 1 || got[0].ID != bare.ID || got[0].ActorID != "" || got[0].TeamID != "" ||
		got[0].TargetType != "" || got[0].Details == nil || len(got[0].Details) != 0 {
		t.Fatalf("empty fields must round-trip as empty: %+v", got)
	}
}

func testPaging(t *testing.T, b Backend) {
	team, other := b.NewID(), b.NewID()
	var all []*domain.Event
	// Pairs share a timestamp, so the ID decides their order.
	for i := range 7 {
		e := appendEvent(t, b, &domain.Event{TeamID: team, Action: "todo.updated", CreatedAt: base.Add(time.Duration(i/2) * time.Second)})
		all = append(all, e)
	}
	appendEvent(t, b, &domain.Event{TeamID: other, Action: "todo.updated", CreatedAt: base})
	slices.SortFunc(all, domain.Compare)

	var pages [][]string
	var cursor *domain.Cursor
	for range 10 {
		page := list(t, b, domain.Filter{TeamID: team, Before: cursor, Limit: 3})
		if len(page) == 0 {
			break
		}
		var ids []string
		for _, e := range page {
			ids = append(ids, e.ID)
		}
		pages = append(pages, ids)
		c := domain.CursorOf(page[len(page)-1])
		cursor = &c
	}
	var got []string
	for _, p := range pages {
		got = append(got, p...)
	}
	want := []string{}
	for _, e := range all {
		want = append(want, e.ID)
	}
	if !slices.Equal(got, want) || len(pages) != 3 {
		t.Fatalf("paged %v (%d pages)\nwant %v", got, len(pages), want)
	}
}

func testDeletes(t *testing.T, b Backend) {
	team, user, keepTeam, keepUser := b.NewID(), b.NewID(), b.NewID(), b.NewID()
	for _, e := range []*domain.Event{
		{TeamID: team, Action: "a"}, {TeamID: keepTeam, Action: "a"},
		{UserID: user, Action: "a"}, {UserID: keepUser, Action: "a"},
	} {
		e.CreatedAt = base
		appendEvent(t, b, e)
	}
	if err := b.Events.DeleteByTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	if err := b.Events.DeleteByUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	for id, f := range map[string]domain.Filter{
		"team": {TeamID: team, Limit: 10}, "user": {UserID: user, Limit: 10},
	} {
		if got := list(t, b, f); len(got) != 0 {
			t.Errorf("%s log not deleted: %d left", id, len(got))
		}
	}
	if len(list(t, b, domain.Filter{TeamID: keepTeam, Limit: 10})) != 1 || len(list(t, b, domain.Filter{UserID: keepUser, Limit: 10})) != 1 {
		t.Fatal("deletes removed other logs")
	}
	if err := b.Events.DeleteByUser(ctx, user); err != nil {
		t.Fatalf("deleting nothing must not fail: %v", err)
	}
}

func testTransactions(t *testing.T, b Backend) {
	user := b.NewID()
	boom := errors.New("boom")
	err := b.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := b.Events.Append(ctx, &domain.Event{UserID: user, Action: "a", CreatedAt: base}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if got := list(t, b, domain.Filter{UserID: user, Limit: 10}); len(got) != 0 {
		t.Fatal("event appended in a failed transaction is visible")
	}
}
