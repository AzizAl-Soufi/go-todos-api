// Package domain holds the audit module's entity: an immutable record of who
// did what, either in a team (the team audit log) or to a user's own account
// (the personal security log).
package domain

import (
	"cmp"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

var (
	ErrInvalidEvent  = apperrors.Internal("INVALID_AUDIT_EVENT", "audit event needs an action and exactly one of team or user")
	ErrInvalidCursor = apperrors.Validation("INVALID_CURSOR", "cursor must be a nextCursor value from a previous page")
	ErrInvalidLimit  = apperrors.Validation("INVALID_LIMIT", "limit must be between 1 and 200")
)

// Event is one audit entry. Exactly one of TeamID (a team event) and UserID (a
// security event about that user's account) is set.
type Event struct {
	ID      string
	TeamID  string
	UserID  string
	ActorID string // who did it; "" when unknown (e.g. a failed login)
	// Action is "<area>.<verb>", e.g. "member.role_changed", "session.login".
	Action     string
	TargetType string // e.g. "todo", "member"; "" when the action has none
	TargetID   string
	// Details are small facts about the change (a title, the changed fields,
	// old and new role). Values are strings so every backend stores them alike.
	Details   map[string]string
	IP        string
	UserAgent string
	CreatedAt time.Time
}

// Validate reports ErrInvalidEvent for an event no log could hold.
func (e *Event) Validate() error {
	if strings.TrimSpace(e.Action) == "" || (e.TeamID == "") == (e.UserID == "") {
		return ErrInvalidEvent
	}
	return nil
}

// Cursor marks the last event of a page; the next page starts after it.
type Cursor struct {
	At time.Time
	ID string
}

func CursorOf(e *Event) Cursor { return Cursor{At: e.CreatedAt, ID: e.ID} }

// String encodes the cursor for clients, who must treat it as opaque.
func (c Cursor) String() string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(c.At.UnixMilli(), 10) + "_" + c.ID))
}

func ParseCursor(s string) (*Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	ms, id, ok := strings.Cut(string(raw), "_")
	millis, err := strconv.ParseInt(ms, 10, 64)
	if !ok || err != nil || id == "" {
		return nil, ErrInvalidCursor
	}
	return &Cursor{At: time.UnixMilli(millis).UTC(), ID: id}, nil
}

// Filter selects one log, newest first. It is the single definition of what
// a page holds: the memory store applies it directly and the database stores
// translate it.
type Filter struct {
	TeamID string // the team log, or
	UserID string // the user's security log
	// Before keeps events that come after the cursor (older ones).
	Before *Cursor
	Limit  int
}

func (f Filter) Matches(e *Event) bool {
	switch {
	case f.TeamID != "" && e.TeamID != f.TeamID,
		f.UserID != "" && e.UserID != f.UserID,
		f.Before != nil && Compare(e, &Event{CreatedAt: f.Before.At, ID: f.Before.ID}) <= 0:
		return false
	}
	return true
}

// Compare orders newest first, then by ID descending.
func Compare(a, b *Event) int {
	return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
}

// Timestamp normalizes a time to UTC milliseconds, which every backend keeps.
func Timestamp(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }
