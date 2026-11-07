// Package domain holds the todos module's entities (todos, projects,
// collections) and their invariants. It has no knowledge of HTTP, storage or
// other modules.
package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

const (
	MaxTitleLength = 500
	MaxNotesLength = 10_000
	MaxTags        = 20
	MaxTagLength   = 50
)

var (
	ErrInvalidTitle    = apperrors.Validation("INVALID_TITLE", "invalid title value")
	ErrTitleTooLong    = apperrors.Validationf("TITLE_TOO_LONG", "title must be at most %d characters", MaxTitleLength)
	ErrNotesTooLong    = apperrors.Validationf("NOTES_TOO_LONG", "notes must be at most %d characters", MaxNotesLength)
	ErrInvalidStatus   = apperrors.Validation("INVALID_STATUS", "status must be one of pending, in_progress, blocked, done, cancelled")
	ErrInvalidPriority = apperrors.Validation("INVALID_PRIORITY", "priority must be one of none, low, medium, high, urgent")
	ErrInvalidTag      = apperrors.Validationf("INVALID_TAG", "tags must be 1 to %d characters", MaxTagLength)
	ErrTooManyTags     = apperrors.Validationf("TOO_MANY_TAGS", "a todo can have at most %d tags", MaxTags)
)

// Status is where a todo is in its life cycle.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusDone       Status = "done"
	StatusCancelled  Status = "cancelled"
)

// Statuses lists every status, in life-cycle order.
var Statuses = []Status{StatusPending, StatusInProgress, StatusBlocked, StatusDone, StatusCancelled}

func ParseStatus(s string) (Status, error) {
	st := Status(s)
	if !slices.Contains(Statuses, st) {
		return "", ErrInvalidStatus
	}
	return st, nil
}

// Open is true while there is still work to do (not done, not cancelled).
func (s Status) Open() bool { return s != StatusDone && s != StatusCancelled }

// Priority orders todos by importance; a higher value is more important.
type Priority int

const (
	PriorityNone Priority = iota
	PriorityLow
	PriorityMedium
	PriorityHigh
	PriorityUrgent
)

var priorityNames = []string{"none", "low", "medium", "high", "urgent"}

func ParsePriority(s string) (Priority, error) {
	i := slices.Index(priorityNames, s)
	if i < 0 {
		return 0, ErrInvalidPriority
	}
	return Priority(i), nil
}

func (p Priority) Valid() bool { return p >= PriorityNone && p <= PriorityUrgent }

func (p Priority) String() string {
	if !p.Valid() {
		return "none"
	}
	return priorityNames[p]
}

type Todo struct {
	ID string
	// UserID is the creator; for a personal todo also its owner.
	UserID string
	// TeamID is the team the todo belongs to through its project; "" is
	// personal. Inbox todos are always personal.
	TeamID string
	// ProjectID is the project (list) the todo belongs to; "" is the inbox.
	ProjectID   string
	Title       string
	Notes       string
	Status      Status
	Priority    Priority
	Tags        []string
	DueAt       *time.Time
	CompletedAt *time.Time // set while Status is done
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Completed is kept for clients of the earlier boolean API.
func (t *Todo) Completed() bool { return t.Status == StatusDone }

// Overdue is true for an open todo whose deadline has passed.
func (t *Todo) Overdue(now time.Time) bool {
	return t.Status.Open() && t.DueAt != nil && t.DueAt.Before(now)
}

// New creates a todo owned by userID from the fields set in c, the same
// Changes an update takes; the title is required. Unset fields get defaults:
// pending, no priority, no tags, inbox. The ID is assigned on persistence.
func New(userID string, c Changes, now time.Time) (*Todo, error) {
	if c.Title == nil {
		return nil, ErrInvalidTitle
	}
	now = Timestamp(now)
	t := &Todo{UserID: userID, Status: StatusPending, Tags: []string{}, CreatedAt: now, UpdatedAt: now}
	if err := t.apply(c, now); err != nil {
		return nil, err
	}
	return t, nil
}

// Patch is an optional field of a partial update that can also be cleared:
// Set reports whether the field was sent, and a nil Value clears it.
type Patch[T any] struct {
	Set   bool
	Value *T
}

func Set[T any](v T) Patch[T] { return Patch[T]{Set: true, Value: &v} }
func Clear[T any]() Patch[T]  { return Patch[T]{Set: true} }

// Get returns the assigned value; ok is false when unset or cleared.
func (p Patch[T]) Get() (T, bool) {
	var zero T
	if !p.Set || p.Value == nil {
		return zero, false
	}
	return *p.Value, true
}

// Changes is a partial update; nil (or unset) fields are left untouched.
type Changes struct {
	Title    *string
	Notes    *string
	Status   *Status
	Priority *Priority
	Tags     *[]string
	DueAt    Patch[time.Time]
	// ProjectID moves the todo; clearing it moves the todo to the inbox.
	ProjectID Patch[string]
	// Completed is the earlier boolean API: true means done; false reopens a
	// done todo as pending. Status wins when both are sent.
	Completed *bool
}

func (c Changes) Empty() bool { return len(c.Fields()) == 0 }

// Fields names the fields c changes, in API spelling (for the audit log).
func (c Changes) Fields() []string {
	var out []string
	for name, set := range map[string]bool{
		"title": c.Title != nil, "notes": c.Notes != nil, "status": c.Status != nil || c.Completed != nil,
		"priority": c.Priority != nil, "tags": c.Tags != nil, "dueAt": c.DueAt.Set, "projectId": c.ProjectID.Set,
	} {
		if set {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// Apply validates and applies c to t at time now. On error t is unchanged.
func (t *Todo) Apply(c Changes, now time.Time) error {
	if c.Empty() {
		return nil
	}
	return t.apply(c, Timestamp(now))
}

func (t *Todo) apply(c Changes, now time.Time) error {
	next := *t
	for _, err := range []error{
		set(&next.Title, c.Title, NormalizeTitle),
		set(&next.Notes, c.Notes, validateNotes),
		set(&next.Priority, c.Priority, validatePriority),
		set(&next.Tags, c.Tags, NormalizeTags),
	} {
		if err != nil {
			return err
		}
	}
	if c.DueAt.Set {
		next.DueAt = nil
		if due, ok := c.DueAt.Get(); ok {
			due = Timestamp(due)
			next.DueAt = &due
		}
	}
	if c.ProjectID.Set {
		next.ProjectID, _ = c.ProjectID.Get()
	}
	status, err := c.nextStatus(next.Status)
	if err != nil {
		return err
	}
	next.setStatus(status, now)
	next.UpdatedAt = now
	*t = next
	return nil
}

// set normalizes and assigns *v to *dst when the field was sent (v != nil).
func set[T any](dst, v *T, normalize func(T) (T, error)) error {
	if v == nil {
		return nil
	}
	n, err := normalize(*v)
	if err == nil {
		*dst = n
	}
	return err
}

func validateNotes(notes string) (string, error) {
	if utf8.RuneCountInString(notes) > MaxNotesLength {
		return "", ErrNotesTooLong
	}
	return notes, nil
}

func validatePriority(p Priority) (Priority, error) {
	if !p.Valid() {
		return 0, ErrInvalidPriority
	}
	return p, nil
}

// nextStatus resolves the status after c: an explicit status wins over the
// earlier completed flag (true: done; false: reopen a done todo).
func (c Changes) nextStatus(current Status) (Status, error) {
	switch {
	case c.Status != nil:
		return ParseStatus(string(*c.Status))
	case c.Completed != nil && *c.Completed:
		return StatusDone, nil
	case c.Completed != nil && current == StatusDone:
		return StatusPending, nil
	}
	return current, nil
}

// setStatus moves to status, keeping completedAt set exactly while done.
func (t *Todo) setStatus(status Status, now time.Time) {
	if status == t.Status && (status != StatusDone || t.CompletedAt != nil) {
		return
	}
	t.Status, t.CompletedAt = status, nil
	if status == StatusDone {
		t.CompletedAt = &now
	}
}

// NormalizeTitle trims a title and validates what is left.
func NormalizeTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", ErrInvalidTitle
	case utf8.RuneCountInString(title) > MaxTitleLength:
		return "", ErrTitleTooLong
	}
	return title, nil
}

// NormalizeTags trims each tag and drops duplicates, keeping the first
// spelling and the given order.
func NormalizeTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || utf8.RuneCountInString(tag) > MaxTagLength {
			return nil, ErrInvalidTag
		}
		if !slices.Contains(out, tag) {
			out = append(out, tag)
		}
	}
	if len(out) > MaxTags {
		return nil, ErrTooManyTags
	}
	return out, nil
}

// Timestamp normalizes a time to UTC at millisecond precision, the finest
// precision every backend (MongoDB included) keeps, so values round-trip
// unchanged and sort the same everywhere.
func Timestamp(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }
