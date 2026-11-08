package domain

import (
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"
)

// DueAlertKind is which deadline reminder a DueAlert is.
type DueAlertKind string

const (
	DueSoon DueAlertKind = "due_soon"
	Overdue DueAlertKind = "overdue"
)

// DueAlert reminds a user of a todo's deadline: it is due soon, or overdue.
// It goes to UserID only (the todos module picks the recipient).
type DueAlert struct {
	UserID    string
	TodoID    string
	Title     string
	TeamID    string // "" for a personal todo
	ProjectID string // "" for the inbox
	Kind      DueAlertKind
	DueAt     time.Time
}

// Key identifies the alert, so it is sent at most once per todo, kind and
// deadline: e.g. "todo-due-soon:<todoID>:<dueAt unix>".
func (e DueAlert) Key() string {
	prefix := "todo-due-soon"
	if e.Kind == Overdue {
		prefix = "todo-overdue"
	}
	return prefix + ":" + e.TodoID + ":" + strconv.FormatInt(e.DueAt.Unix(), 10)
}

// Message is what the user sees at now: "Due soon: Ship it" / "Due in 3
// hours", or "Overdue: Ship it" / "Was due 2 days ago". The wording is
// relative, so it needs no time zone.
func (e DueAlert) Message(now time.Time) Message {
	prefix, body, typ := "Due soon: ", dueIn(e.DueAt.Sub(now)), "todo.due_soon"
	if e.Kind == Overdue {
		prefix, body, typ = "Overdue: ", wasDue(now.Sub(e.DueAt)), "todo.overdue"
	}
	data := map[string]string{"type": typ, "todoId": e.TodoID}
	if e.TeamID != "" {
		data["teamId"] = e.TeamID
	}
	if e.ProjectID != "" {
		data["projectId"] = e.ProjectID
	}
	title := truncate(e.Title, MaxTitleLength-utf8.RuneCountInString(prefix))
	return Message{
		Title: prefix + title,
		Body:  truncate(body, MaxBodyLength),
		Data:  data,
	}
}

func dueIn(d time.Duration) string {
	if d < time.Minute {
		return "Due now"
	}
	return "Due in " + span(d)
}

func wasDue(d time.Duration) string {
	if d < time.Minute {
		return "Due just now"
	}
	return "Was due " + span(d) + " ago"
}

// span is d in whole minutes, hours or days (rounded down), e.g. "3 hours".
func span(d time.Duration) string {
	switch {
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour")
	}
	return plural(int(d/(24*time.Hour)), "day")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
