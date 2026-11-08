package domain

import "time"

// DueAlertKind is a reminder about a todo's deadline.
type DueAlertKind string

const (
	// AlertDueSoon: the deadline is within DueSoonWindow.
	AlertDueSoon DueAlertKind = "due_soon"
	// AlertOverdue: the deadline has passed.
	AlertOverdue DueAlertKind = "overdue"
)

// DueSoonWindow is how far ahead of a deadline the due-soon alert goes out.
const DueSoonWindow = 24 * time.Hour

// DueAlertsSent remembers, per kind, the deadline an alert was last sent
// for (nil: never). Remembering the deadline rather than a flag re-arms
// both alerts when the deadline moves.
type DueAlertsSent struct {
	DueSoonFor *time.Time
	OverdueFor *time.Time
}

// For returns the deadline the alert of kind was sent for.
func (s DueAlertsSent) For(kind DueAlertKind) *time.Time {
	if kind == AlertOverdue {
		return s.OverdueFor
	}
	return s.DueSoonFor
}

// NextDueAlert reports which alert an open todo with a deadline needs at
// now, given what was sent, if any (archived projects aside, which the
// stores exclude):
//
//   - overdue once the deadline has passed (as Todo.Overdue), unless sent
//     for this deadline;
//   - due soon while the deadline is at most DueSoonWindow away, unless sent
//     for this deadline.
//
// Once the overdue alert went out for a deadline nothing more is due for
// it. A todo first seen after its deadline gets only the overdue alert.
func NextDueAlert(t *Todo, sent DueAlertsSent, now time.Time) (DueAlertKind, bool) {
	if !t.Status.Open() || t.DueAt == nil {
		return "", false
	}
	due := *t.DueAt
	switch {
	case sentFor(sent.OverdueFor, due):
		return "", false
	case due.Before(now):
		return AlertOverdue, true
	case due.After(now.Add(DueSoonWindow)) || sentFor(sent.DueSoonFor, due):
		return "", false
	}
	return AlertDueSoon, true
}

func sentFor(sent *time.Time, due time.Time) bool { return sent != nil && sent.Equal(due) }
