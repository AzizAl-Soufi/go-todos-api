package domain

import (
	"maps"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestDueAlert_Message(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	personal := DueAlert{UserID: "u1", TodoID: "td1", Title: "Ship it", Kind: DueSoon}
	team := DueAlert{UserID: "u1", TodoID: "td1", Title: "Ship it", TeamID: "t1", ProjectID: "p1", Kind: Overdue}

	cases := []struct {
		name        string
		alert       DueAlert
		due         time.Duration // from now
		title, body string
		data        map[string]string
	}{
		{"due in hours", personal, 3*time.Hour + 59*time.Minute, "Due soon: Ship it", "Due in 3 hours",
			map[string]string{"type": "todo.due_soon", "todoId": "td1"}},
		{"due in one hour", personal, time.Hour, "Due soon: Ship it", "Due in 1 hour", nil},
		{"due in minutes", personal, 45 * time.Minute, "Due soon: Ship it", "Due in 45 minutes", nil},
		{"due now", personal, 20 * time.Second, "Due soon: Ship it", "Due now", nil},
		{"due in a day", personal, 24 * time.Hour, "Due soon: Ship it", "Due in 1 day", nil},
		{"overdue by days", team, -(2*24*time.Hour + 5*time.Hour), "Overdue: Ship it", "Was due 2 days ago",
			map[string]string{"type": "todo.overdue", "todoId": "td1", "teamId": "t1", "projectId": "p1"}},
		{"overdue by a minute", team, -time.Minute, "Overdue: Ship it", "Was due 1 minute ago", nil},
		{"just overdue", team, -10 * time.Second, "Overdue: Ship it", "Due just now", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.alert.DueAt = now.Add(c.due)
			m := c.alert.Message(now)
			if m.Title != c.title || m.Body != c.body {
				t.Fatalf("message = %q / %q, want %q / %q", m.Title, m.Body, c.title, c.body)
			}
			if c.data != nil && !maps.Equal(m.Data, c.data) {
				t.Fatalf("data = %v, want %v", m.Data, c.data)
			}
		})
	}
}

func TestDueAlert_MessageIsShortened(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for _, kind := range []DueAlertKind{DueSoon, Overdue} {
		e := DueAlert{TodoID: "td1", Title: strings.Repeat("🚀", 500), Kind: kind, DueAt: now.Add(-time.Hour)}
		m := e.Message(now)
		if n := utf8.RuneCountInString(m.Title); n > MaxTitleLength || !strings.HasSuffix(m.Title, "…") {
			t.Fatalf("%s title has %d characters: %q", kind, n, m.Title)
		}
		if utf8.RuneCountInString(m.Body) > MaxBodyLength {
			t.Fatalf("%s body too long: %q", kind, m.Body)
		}
	}
}

func TestDueAlert_Key(t *testing.T) {
	due := time.Unix(1_772_366_400, 0).UTC()
	soon := DueAlert{TodoID: "td1", Kind: DueSoon, DueAt: due}
	over := DueAlert{TodoID: "td1", Kind: Overdue, DueAt: due}
	if soon.Key() != "todo-due-soon:td1:1772366400" || over.Key() != "todo-overdue:td1:1772366400" {
		t.Fatalf("keys = %q, %q", soon.Key(), over.Key())
	}
	moved := soon
	moved.DueAt = due.Add(time.Hour)
	if moved.Key() == soon.Key() {
		t.Fatal("a new deadline must get a new key")
	}
}
