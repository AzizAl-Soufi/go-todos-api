package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func TestNew(t *testing.T) {
	todo, err := New("u1", Changes{Title: ptr("  buy milk ")}, now)
	if err != nil {
		t.Fatal(err)
	}
	if todo.UserID != "u1" || todo.Title != "buy milk" || todo.Status != StatusPending || todo.Completed() ||
		todo.Priority != PriorityNone || todo.Tags == nil || !todo.CreatedAt.Equal(now) || !todo.UpdatedAt.Equal(now) {
		t.Fatalf("unexpected todo: %+v", todo)
	}

	done, err := New("u1", Changes{Title: ptr("x"), Status: ptr(StatusDone), DueAt: Set(now.Add(time.Hour)), Tags: &[]string{" a ", "a", "b"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if done.CompletedAt == nil || !done.CompletedAt.Equal(now) || !slices.Equal(done.Tags, []string{"a", "b"}) || done.DueAt == nil {
		t.Fatalf("unexpected todo: %+v", done)
	}

	x := ptr("x")
	for name, d := range map[string]Changes{
		"missing title": {},
		"blank title":   {Title: ptr("   ")},
		"long title":    {Title: ptr(strings.Repeat("x", MaxTitleLength+1))},
		"long notes":    {Title: x, Notes: ptr(strings.Repeat("n", MaxNotesLength+1))},
		"bad status":    {Title: x, Status: ptr(Status("later"))},
		"bad priority":  {Title: x, Priority: ptr(Priority(9))},
		"empty tag":     {Title: x, Tags: &[]string{" "}},
	} {
		if _, err := New("u1", d, now); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestApply_StatusAndCompletion(t *testing.T) {
	todo, _ := New("u1", Changes{Title: ptr("x")}, now)
	later := now.Add(time.Hour)

	if err := todo.Apply(Changes{Status: ptr(StatusInProgress)}, later); err != nil || todo.CompletedAt != nil || !todo.UpdatedAt.Equal(later) {
		t.Fatalf("in progress: %+v, %v", todo, err)
	}
	if err := todo.Apply(Changes{Status: ptr(StatusDone)}, later); err != nil || todo.CompletedAt == nil || !todo.Completed() {
		t.Fatalf("done: %+v, %v", todo, err)
	}
	doneAt := *todo.CompletedAt
	todo.Apply(Changes{Title: ptr("renamed")}, later.Add(time.Hour))
	if !todo.CompletedAt.Equal(doneAt) {
		t.Fatal("editing a done todo must keep its completion time")
	}
	if err := todo.Apply(Changes{Status: ptr(StatusBlocked)}, later); err != nil || todo.CompletedAt != nil {
		t.Fatalf("reopened todo keeps completedAt: %+v", todo)
	}
}

func TestApply_CompletedFlag(t *testing.T) {
	todo, _ := New("u1", Changes{Title: ptr("x"), Status: ptr(StatusInProgress)}, now)
	todo.Apply(Changes{Completed: ptr(true)}, now)
	if todo.Status != StatusDone {
		t.Fatalf("completed=true: status %s", todo.Status)
	}
	todo.Apply(Changes{Completed: ptr(false)}, now)
	if todo.Status != StatusPending {
		t.Fatalf("completed=false on a done todo: status %s", todo.Status)
	}
	todo.Apply(Changes{Status: ptr(StatusBlocked)}, now)
	todo.Apply(Changes{Completed: ptr(false)}, now)
	if todo.Status != StatusBlocked {
		t.Fatal("completed=false must not touch a todo that isn't done")
	}
	todo.Apply(Changes{Completed: ptr(true), Status: ptr(StatusInProgress)}, now)
	if todo.Status != StatusInProgress {
		t.Fatal("status wins over completed")
	}
}

func TestApply_PatchesAndAtomicity(t *testing.T) {
	todo, _ := New("u1", Changes{Title: ptr("x"), ProjectID: Set("p1"), DueAt: Set(now)}, now)
	todo.Apply(Changes{DueAt: Clear[time.Time](), ProjectID: Clear[string]()}, now)
	if todo.DueAt != nil || todo.ProjectID != "" {
		t.Fatalf("clearing patches: %+v", todo)
	}
	todo.Apply(Changes{ProjectID: Set("p2"), DueAt: Set(now.Add(time.Hour))}, now)
	if todo.ProjectID != "p2" || todo.DueAt == nil {
		t.Fatalf("setting patches: %+v", todo)
	}

	before := *todo
	err := todo.Apply(Changes{Title: ptr("new"), Tags: &[]string{strings.Repeat("t", MaxTagLength+1)}}, now.Add(time.Hour))
	if !errors.Is(err, ErrInvalidTag) || todo.Title != before.Title || !todo.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("a failed update must leave the todo unchanged: %+v, %v", todo, err)
	}
	if err := todo.Apply(Changes{}, now.Add(time.Hour)); err != nil || !todo.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("an empty update changes nothing")
	}
}

func TestNormalizeTags(t *testing.T) {
	many := make([]string, MaxTags+1)
	for i := range many {
		many[i] = strings.Repeat("t", i+1)
	}
	if _, err := NormalizeTags(many); !errors.Is(err, ErrTooManyTags) {
		t.Fatalf("err = %v", err)
	}
}

func TestParsers(t *testing.T) {
	for _, s := range []string{"pending", "in_progress", "blocked", "done", "cancelled"} {
		if _, err := ParseStatus(s); err != nil {
			t.Errorf("status %q: %v", s, err)
		}
	}
	if _, err := ParseStatus("todo"); !errors.Is(err, ErrInvalidStatus) {
		t.Error("unknown status accepted")
	}
	for i, s := range []string{"none", "low", "medium", "high", "urgent"} {
		if p, err := ParsePriority(s); err != nil || int(p) != i || p.String() != s {
			t.Errorf("priority %q = %d, %v", s, p, err)
		}
	}
	if _, err := ParseSort("title"); !errors.Is(err, ErrInvalidSort) {
		t.Error("unknown sort accepted")
	}
	if s, _ := ParseSort(""); s != SortCreated {
		t.Errorf("default sort = %q", s)
	}
}

func TestMeta(t *testing.T) {
	if _, err := NewMeta(MetaChanges{}, now); err != ErrInvalidName {
		t.Fatalf("missing name: %v", err)
	}
	m, err := NewMeta(MetaChanges{Name: ptr(" Work "), Icon: ptr("💼")}, now)
	if err != nil || m.Name != "Work" || m.Color != "default" || m.Archived() {
		t.Fatalf("new meta: %+v, %v", m, err)
	}
	later := now.Add(time.Hour)
	if err := m.Apply(MetaChanges{Archived: ptr(true), Color: ptr("blue")}, later); err != nil || !m.Archived() || m.Color != "blue" {
		t.Fatalf("archive: %+v, %v", m, err)
	}
	m.Apply(MetaChanges{Archived: ptr(false)}, later)
	if m.Archived() {
		t.Fatal("unarchive failed")
	}
	for name, c := range map[string]MetaChanges{
		"blank name": {Name: ptr(" ")},
		"long name":  {Name: ptr(strings.Repeat("n", MaxNameLength+1))},
		"long desc":  {Description: ptr(strings.Repeat("d", MaxDescriptionLength+1))},
		"long icon":  {Icon: ptr(strings.Repeat("i", MaxIconBytes+1))},
		"bad color":  {Color: ptr("teal")},
	} {
		if err := m.Apply(c, later); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestQuery(t *testing.T) {
	mk := func(title string, p Priority, due *time.Time, created time.Duration) *Todo {
		return &Todo{Title: title, Status: StatusPending, Priority: p, DueAt: due, CreatedAt: now.Add(created), UpdatedAt: now}
	}
	soon, later := ptr(now.Add(time.Hour)), ptr(now.Add(2*time.Hour))
	a, b, c, d := mk("a", PriorityLow, later, 0), mk("b", PriorityHigh, nil, time.Minute), mk("c", PriorityHigh, soon, 2*time.Minute), mk("d", PriorityNone, soon, 3*time.Minute)
	order := func(q Query) string {
		list := []*Todo{a, b, c, d}
		slices.SortFunc(list, q.Compare)
		out := ""
		for _, t := range list {
			out += t.Title
		}
		return out
	}
	for sort, want := range map[Sort]string{SortCreated: "dcba", SortDue: "dcab", SortPriority: "cbad"} {
		if got := order(Query{Sort: sort}); got != want {
			t.Errorf("sort %s = %s, want %s", sort, got, want)
		}
	}

	overdue := &Todo{Status: StatusBlocked, DueAt: ptr(now.Add(-time.Minute))}
	if !(Query{OverdueAt: &now}).Matches(overdue) {
		t.Error("open todo past its deadline is overdue")
	}
	overdue.Status = StatusCancelled
	if (Query{OverdueAt: &now}).Matches(overdue) {
		t.Error("cancelled todo is never overdue")
	}
	if !(Query{Search: "NOTE"}).Matches(&Todo{Title: "x", Notes: "a note"}) {
		t.Error("search must be case-insensitive and cover notes")
	}
	if (Query{DueBefore: &now}).Matches(&Todo{}) {
		t.Error("todos without a deadline don't match deadline filters")
	}
}

func TestProgress(t *testing.T) {
	var p Progress
	p.AddCount(StatusDone, 2, 0)
	p.AddCount(StatusPending, 1, 1)
	p.AddCount(StatusCancelled, 1, 0)
	if p.Total != 4 || p.Overdue != 1 || p.PercentDone() != 66 {
		t.Fatalf("progress = %+v, %d%%", p, p.PercentDone())
	}
	var sum Progress
	sum.Add(p)
	sum.Add(p)
	if sum.Total != 8 || sum.Overdue != 2 || sum.PercentDone() != 66 {
		t.Fatalf("sum = %+v", sum)
	}
	if (Progress{}).PercentDone() != 0 {
		t.Fatal("empty progress is 0%")
	}
}
