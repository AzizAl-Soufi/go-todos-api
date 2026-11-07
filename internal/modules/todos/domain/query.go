package domain

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

var ErrInvalidSort = apperrors.Validation("INVALID_SORT", "sort must be one of created, updated, due, priority")

// Sort orders a todo list. Every order ends with newest first and then by
// ID (descending), so results are stable and identical across backends.
type Sort string

const (
	SortCreated  Sort = "created"  // newest first (default)
	SortUpdated  Sort = "updated"  // most recently changed first
	SortDue      Sort = "due"      // soonest deadline first, no deadline last
	SortPriority Sort = "priority" // most important first, then soonest deadline
)

func ParseSort(s string) (Sort, error) {
	sort := Sort(s)
	switch sort {
	case "":
		return SortCreated, nil
	case SortCreated, SortUpdated, SortDue, SortPriority:
		return sort, nil
	}
	return "", ErrInvalidSort
}

// Query selects and orders the todos in scope. Zero values don't filter. It is the
// single definition of filter semantics: the memory store applies it
// directly, and the database stores translate it (the store contract tests
// check they agree).
type Query struct {
	OwnerFilter
	// ProjectID limits to one project; Inbox limits to todos without one.
	ProjectID string
	Inbox     bool
	// ExcludeProjectIDs hides todos of these projects (e.g. archived ones).
	ExcludeProjectIDs []string

	Statuses   []Status
	Priorities []Priority
	Tag        string     // exact tag
	DueBefore  *time.Time // deadline strictly before
	DueAfter   *time.Time // deadline at or after
	// OverdueAt, when set, keeps open todos whose deadline is before it.
	OverdueAt *time.Time
	// Search is a case-insensitive substring of the title or notes.
	Search string

	Sort Sort
}

func (q Query) Matches(t *Todo) bool {
	switch {
	case !q.OwnerFilter.Matches(t.TeamID),
		q.ProjectID != "" && t.ProjectID != q.ProjectID,
		q.Inbox && t.ProjectID != "",
		t.ProjectID != "" && slices.Contains(q.ExcludeProjectIDs, t.ProjectID),
		len(q.Statuses) > 0 && !slices.Contains(q.Statuses, t.Status),
		len(q.Priorities) > 0 && !slices.Contains(q.Priorities, t.Priority),
		q.Tag != "" && !slices.Contains(t.Tags, q.Tag),
		q.DueBefore != nil && (t.DueAt == nil || !t.DueAt.Before(*q.DueBefore)),
		q.DueAfter != nil && (t.DueAt == nil || t.DueAt.Before(*q.DueAfter)),
		q.OverdueAt != nil && !t.Overdue(*q.OverdueAt):
		return false
	}
	if q.Search != "" {
		needle := strings.ToLower(q.Search)
		return strings.Contains(strings.ToLower(t.Title), needle) || strings.Contains(strings.ToLower(t.Notes), needle)
	}
	return true
}

// Compare orders a before b (negative) or after it (positive) for q.Sort.
func (q Query) Compare(a, b *Todo) int {
	newestFirst := func() int { return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID)) }
	switch q.Sort {
	case SortUpdated:
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), newestFirst())
	case SortDue:
		return cmp.Or(compareDue(a, b), newestFirst())
	case SortPriority:
		return cmp.Or(cmp.Compare(b.Priority, a.Priority), compareDue(a, b), newestFirst())
	default:
		return newestFirst()
	}
}

// compareDue puts earlier deadlines first and todos without one last.
func compareDue(a, b *Todo) int {
	switch {
	case a.DueAt == nil && b.DueAt == nil:
		return 0
	case a.DueAt == nil:
		return 1
	case b.DueAt == nil:
		return -1
	}
	return a.DueAt.Compare(*b.DueAt)
}
