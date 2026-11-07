package domain

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
)

func TestNewDevice(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("x", 3600))
	d, err := NewDevice("u1", "  fcm:token-1  ", " iOS ", now)
	if err != nil {
		t.Fatal(err)
	}
	normalized := d.Token == "fcm:token-1" && d.Platform == IOS
	inUTC := d.CreatedAt.Location() == time.UTC
	if !normalized || !inUTC || !d.UpdatedAt.Equal(d.CreatedAt) {
		t.Fatalf("NewDevice = %+v", d)
	}
	invalid := []string{
		"",
		"  ",
		"has space",
		"nul\x00byte",
		"tab\tinside",
		"non-ascii-é",
		strings.Repeat("x", MaxTokenLength+1),
	}
	for _, bad := range invalid {
		if _, err := NewDevice("u1", bad, "web", now); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("token %q: err = %v", bad, err)
		}
	}
	if _, err := NewDevice("u1", "tok", "windows", now); !errors.Is(err, ErrInvalidPlatform) {
		t.Fatalf("platform: err = %v", err)
	}
}

func TestTodoCreated(t *testing.T) {
	e := TodoCreated{
		TeamID:    "t1",
		ProjectID: "p1",
		TodoID:    "td1",
		Title:     "Ship it",
		ActorID:   "ann",
		ActorName: "Ann",
	}
	notifies := map[access.Role]bool{
		access.Owner:  true,
		access.Admin:  true,
		access.Member: false,
		access.Viewer: false,
	}
	for role, want := range notifies {
		e.ActorRole = role
		if e.Notifies() != want {
			t.Errorf("%s adds a task: notifies = %v, want %v", role, !want, want)
		}
	}
	e.ActorRole = access.Admin
	if personal := (TodoCreated{ActorRole: access.Owner}); personal.Notifies() {
		t.Fatal("a todo without a team notifies nobody")
	}

	roster := access.Roster{
		Name:    "Platform",
		Members: map[string]access.Role{"ann": access.Admin, "bo": access.Member, "cy": access.Viewer},
	}
	if got := slices.Sorted(slices.Values(e.Recipients(roster))); !slices.Equal(got, []string{"bo", "cy"}) {
		t.Fatalf("recipients = %v, want everyone but the actor", got)
	}

	m := e.Message("Platform")
	wantData := map[string]string{"type": "todo.created", "teamId": "t1", "projectId": "p1", "todoId": "td1"}
	textOK := m.Title == "New task in Platform" && m.Body == "Ann added “Ship it”"
	if !textOK || !maps.Equal(m.Data, wantData) {
		t.Fatalf("message = %+v", m)
	}
	e.ActorName = " "
	if m := e.Message("Platform"); !strings.HasPrefix(m.Body, "An admin added") {
		t.Fatalf("message without a name = %q", m.Body)
	}
}

func TestDeviceID(t *testing.T) {
	d := &Device{Token: "fcm:token-1"}
	id := d.ID()
	if len(id) != 32 || strings.Contains(id, "token") || id != DeviceID("fcm:token-1") {
		t.Fatalf("ID = %q, want 32 hex characters derived from the token", id)
	}
	if DeviceID("fcm:token-2") == id {
		t.Fatal("different tokens must have different IDs")
	}
}

func TestTodoCreated_MessageIsShortened(t *testing.T) {
	e := TodoCreated{
		TeamID:    "t1",
		Title:     strings.Repeat("🚀", 500),
		ActorName: strings.Repeat("A", 100),
		ActorRole: access.Admin,
	}
	m := e.Message(strings.Repeat("T", 100))
	title, body := []rune(m.Title), []rune(m.Body)
	if len(title) > MaxTitleLength || title[len(title)-1] != '…' {
		t.Fatalf("title has %d characters: %q", len(title), m.Title)
	}
	if len(body) > MaxBodyLength || !strings.HasSuffix(m.Body, "…”") {
		t.Fatalf("body has %d characters: %q", len(body), m.Body)
	}
	if !strings.HasPrefix(m.Body, strings.Repeat("A", maxActorLength-1)+"… added “🚀") {
		t.Fatalf("body = %q, want the shortened name, then the task", m.Body)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exactly", 7, "exactly"},
		{"too long", 4, "too…"},
		{"🚀🚀🚀", 2, "🚀…"},
		{"cut", 1, "…"},
		{"no room", 0, ""},
		{"negative", -5, ""},
		{"", 0, ""},
	}
	for _, c := range cases {
		if got := truncate(c.s, c.n); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}
