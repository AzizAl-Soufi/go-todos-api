// Package storetest is the contract every todos-module persistence adapter
// must satisfy; the memory, MongoDB and PostgreSQL stores all run it.
//
// Filtering and ordering are checked against domain.Query itself: each case
// runs the query on the store and compares with what Query.Matches and
// Query.Compare give for the same data, so all backends share one definition.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

// Backend is one set of repositories on empty storage.
type Backend struct {
	Todos       application.TodoStore
	Projects    application.ProjectStore
	Collections application.CollectionStore
	Tx          tx.Runner
	// AtomicTx is true when Tx really rolls back (not tx.None).
	AtomicTx bool
	// NewUser returns the ID of a user records may belong to (PostgreSQL
	// needs a real row for its foreign keys).
	NewUser func(t *testing.T) string
	// MissingID is a well-formed ID for this backend that matches nothing.
	MissingID string
}

// Run runs the contract; newBackend must return fresh, empty storage.
func Run(t *testing.T, newBackend func(t *testing.T) Backend) {
	t.Run("TodoRoundTripAndOwnership", func(t *testing.T) { testTodoCRUD(t, newBackend(t)) })
	t.Run("TodoFiltersAndSorting", func(t *testing.T) { testTodoQueries(t, newBackend(t)) })
	t.Run("TodoProgress", func(t *testing.T) { testProgress(t, newBackend(t)) })
	t.Run("TodoBulkDeletes", func(t *testing.T) { testBulkDeletes(t, newBackend(t)) })
	t.Run("ConcurrentCreates", func(t *testing.T) { testConcurrent(t, newBackend(t)) })
	t.Run("Projects", func(t *testing.T) { testProjects(t, newBackend(t)) })
	t.Run("Collections", func(t *testing.T) { testCollections(t, newBackend(t)) })
	t.Run("Transactions", func(t *testing.T) {
		b := newBackend(t)
		if !b.AtomicTx {
			t.Skip("this backend runs without transactions")
		}
		testTransactions(t, b)
	})
}

var ctx = context.Background()

// base is a fixed, millisecond-precise reference time.
var base = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time { t := base.Add(d); return &t }

func newTodo(userID string, created time.Duration) *domain.Todo {
	return &domain.Todo{
		UserID: userID, Title: "todo", Status: domain.StatusPending, Tags: []string{},
		CreatedAt: base.Add(created), UpdatedAt: base.Add(created),
	}
}

func create(t *testing.T, b Backend, todo *domain.Todo) *domain.Todo {
	t.Helper()
	if err := b.Todos.Create(ctx, todo); err != nil || todo.ID == "" {
		t.Fatalf("create todo: %v, id %q", err, todo.ID)
	}
	return todo
}

func newProject(t *testing.T, b Backend, userID, name, collectionID string, created time.Duration) *domain.Project {
	t.Helper()
	p := &domain.Project{UserID: userID, CollectionID: collectionID, Meta: domain.Meta{
		Name: name, Color: "default", CreatedAt: base.Add(created), UpdatedAt: base.Add(created),
	}}
	if err := b.Projects.Create(ctx, p); err != nil || p.ID == "" {
		t.Fatalf("create project: %v, id %q", err, p.ID)
	}
	return p
}

func newCollection(t *testing.T, b Backend, userID, name string, created time.Duration) *domain.Collection {
	t.Helper()
	c := &domain.Collection{UserID: userID, Meta: domain.Meta{
		Name: name, Color: "default", CreatedAt: base.Add(created), UpdatedAt: base.Add(created),
	}}
	if err := b.Collections.Create(ctx, c); err != nil || c.ID == "" {
		t.Fatalf("create collection: %v, id %q", err, c.ID)
	}
	return c
}

func equalTime(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

func sameTodo(a, b *domain.Todo) bool {
	return a.ID == b.ID && a.UserID == b.UserID && a.ProjectID == b.ProjectID && a.Title == b.Title &&
		a.Notes == b.Notes && a.Status == b.Status && a.Priority == b.Priority && slices.Equal(a.Tags, b.Tags) &&
		equalTime(a.DueAt, b.DueAt) && equalTime(a.CompletedAt, b.CompletedAt) &&
		a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt)
}

func expectNotFound(t *testing.T, cases map[string]error) {
	t.Helper()
	for name, err := range cases {
		if !errors.Is(err, apperrors.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
}

func testTodoCRUD(t *testing.T, b Backend) {
	owner, other := b.NewUser(t), b.NewUser(t)
	project := newProject(t, b, owner, "P", "", 0)

	todo := newTodo(owner, 0)
	todo.ProjectID, todo.Title, todo.Notes = project.ID, "full", "some notes\nline 2"
	todo.Status, todo.Priority, todo.Tags = domain.StatusDone, domain.PriorityHigh, []string{"home", "Errand"}
	todo.DueAt, todo.CompletedAt = at(48*time.Hour), at(time.Hour)
	create(t, b, todo)

	got, err := b.Todos.Get(ctx, owner, todo.ID)
	if err != nil || !sameTodo(got, todo) {
		t.Fatalf("round trip:\n got  %+v (%v)\n want %+v", got, err, todo)
	}

	plain := create(t, b, newTodo(owner, time.Minute))
	if got, _ := b.Todos.Get(ctx, owner, plain.ID); got.ProjectID != "" || got.DueAt != nil || got.Tags == nil || len(got.Tags) != 0 {
		t.Fatalf("empty optional fields must round-trip as empty: %+v", got)
	}

	expectNotFound(t, map[string]error{
		"Get by another user":    func() error { _, err := b.Todos.Get(ctx, other, todo.ID); return err }(),
		"Update by another user": b.Todos.Update(ctx, &domain.Todo{ID: todo.ID, UserID: other, Title: "x", Status: domain.StatusPending}),
		"Delete by another user": b.Todos.Delete(ctx, other, todo.ID),
		"Get missing":            func() error { _, err := b.Todos.Get(ctx, owner, b.MissingID); return err }(),
		"Update missing":         b.Todos.Update(ctx, &domain.Todo{ID: b.MissingID, UserID: owner, Title: "x", Status: domain.StatusPending}),
		"Delete missing":         b.Todos.Delete(ctx, owner, b.MissingID),
	})

	// Update saves every field, including clearing optional ones.
	todo.ProjectID, todo.Title, todo.Notes, todo.Status = "", "changed", "", domain.StatusInProgress
	todo.Priority, todo.Tags, todo.DueAt, todo.CompletedAt, todo.UpdatedAt = domain.PriorityNone, []string{}, nil, nil, base.Add(time.Hour)
	if err := b.Todos.Update(ctx, todo); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Todos.Get(ctx, owner, todo.ID); !sameTodo(got, todo) {
		t.Fatalf("update:\n got  %+v\n want %+v", got, todo)
	}

	if err := b.Todos.Delete(ctx, owner, todo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Todos.Get(ctx, owner, todo.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleted todo: err = %v", err)
	}
}

func testTodoQueries(t *testing.T, b Backend) {
	user, other := b.NewUser(t), b.NewUser(t)
	work := newProject(t, b, user, "Work", "", 0)
	home := newProject(t, b, user, "Home", "", time.Second)

	type spec struct {
		project  *domain.Project
		title    string
		notes    string
		status   domain.Status
		priority domain.Priority
		tags     []string
		due      *time.Time
		updated  time.Duration
	}
	specs := []spec{
		{work, "Write report", "", domain.StatusPending, domain.PriorityHigh, []string{"q1"}, at(-48 * time.Hour), 9},
		{work, "Review PR", "needs CHANGELOG", domain.StatusInProgress, domain.PriorityUrgent, nil, at(24 * time.Hour), 3},
		{work, "Ship 50% off_sale", "", domain.StatusDone, domain.PriorityHigh, []string{"q1", "launch"}, at(-24 * time.Hour), 1},
		{home, "Buy milk", "2 liters", domain.StatusPending, domain.PriorityNone, []string{"errand"}, nil, 7},
		{home, "Fix sink", "", domain.StatusBlocked, domain.PriorityMedium, nil, at(-time.Hour), 5},
		{nil, "Call mom", "", domain.StatusPending, domain.PriorityLow, nil, at(72 * time.Hour), 2},
		{nil, "Old idea", "Report draft", domain.StatusCancelled, domain.PriorityNone, nil, at(-72 * time.Hour), 8},
		{nil, "Read book", "", domain.StatusPending, domain.PriorityHigh, nil, nil, 4},
	}
	var fixtures []*domain.Todo
	for i, s := range specs {
		todo := newTodo(user, time.Duration(i)*time.Minute)
		todo.Title, todo.Notes, todo.Status, todo.Priority, todo.DueAt = s.title, s.notes, s.status, s.priority, s.due
		if s.tags != nil {
			todo.Tags = s.tags
		}
		if s.project != nil {
			todo.ProjectID = s.project.ID
		}
		todo.UpdatedAt = base.Add(time.Hour + s.updated*time.Minute)
		fixtures = append(fixtures, create(t, b, todo))
	}
	create(t, b, newTodo(other, 0)) // another user's todo never shows up

	now := base
	cases := map[string]domain.Query{
		"everything":            {},
		"project":               {ProjectID: work.ID},
		"inbox":                 {Inbox: true},
		"exclude project":       {ExcludeProjectIDs: []string{home.ID}},
		"statuses":              {Statuses: []domain.Status{domain.StatusPending, domain.StatusBlocked}},
		"priorities":            {Priorities: []domain.Priority{domain.PriorityHigh, domain.PriorityUrgent}},
		"tag":                   {Tag: "q1"},
		"tag is exact":          {Tag: "Q1"},
		"due before":            {DueBefore: &now},
		"due after":             {DueAfter: at(-24 * time.Hour)},
		"due window":            {DueAfter: at(-48 * time.Hour), DueBefore: at(24 * time.Hour)},
		"overdue":               {OverdueAt: &now},
		"search title":          {Search: "REPORT"},
		"search notes":          {Search: "liters"},
		"search is literal":     {Search: "50% off_"},
		"search wildcard chars": {Search: "%"},
		"combined":              {ProjectID: work.ID, Statuses: []domain.Status{domain.StatusPending, domain.StatusInProgress}, Search: "r"},
		"sort updated":          {Sort: domain.SortUpdated},
		"sort due":              {Sort: domain.SortDue},
		"sort priority":         {Sort: domain.SortPriority},
		"filter and sort":       {Inbox: true, Sort: domain.SortDue},
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			var want []string
			expected := slices.Clone(fixtures)
			slices.SortFunc(expected, q.Compare)
			for _, todo := range expected {
				if q.Matches(todo) {
					want = append(want, todo.Title)
				}
			}
			list, err := b.Todos.List(ctx, user, q)
			if err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, todo := range list {
				got = append(got, todo.Title)
			}
			if want == nil {
				want = []string{}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("got  %q\nwant %q", got, want)
			}
		})
	}
}

func testProgress(t *testing.T, b Backend) {
	user := b.NewUser(t)
	p := newProject(t, b, user, "P", "", 0)
	add := func(projectID string, status domain.Status, due *time.Time) {
		todo := newTodo(user, 0)
		todo.ProjectID, todo.Status, todo.DueAt = projectID, status, due
		create(t, b, todo)
	}
	add(p.ID, domain.StatusPending, at(-time.Hour)) // overdue
	add(p.ID, domain.StatusPending, at(time.Hour))
	add(p.ID, domain.StatusDone, at(-time.Hour)) // done: not overdue
	add(p.ID, domain.StatusCancelled, nil)
	add("", domain.StatusBlocked, at(-time.Minute)) // inbox, overdue

	got, err := b.Todos.Progress(ctx, user, base)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		p.ID: "total=4 pending=2 done=1 cancelled=1 blocked=0 overdue=1 percent=33",
		"":   "total=1 pending=0 done=0 cancelled=0 blocked=1 overdue=1 percent=0",
	}
	if len(got) != len(want) {
		t.Fatalf("progress keys = %v, want project and inbox", got)
	}
	for key, w := range want {
		pr := got[key]
		s := fmt.Sprintf("total=%d pending=%d done=%d cancelled=%d blocked=%d overdue=%d percent=%d",
			pr.Total, pr.ByStatus[domain.StatusPending], pr.ByStatus[domain.StatusDone], pr.ByStatus[domain.StatusCancelled],
			pr.ByStatus[domain.StatusBlocked], pr.Overdue, pr.PercentDone())
		if s != w {
			t.Errorf("progress[%q] = %s, want %s", key, s, w)
		}
	}
}

func testBulkDeletes(t *testing.T, b Backend) {
	u1, u2 := b.NewUser(t), b.NewUser(t)
	p := newProject(t, b, u1, "P", "", 0)
	inProject := newTodo(u1, 0)
	inProject.ProjectID = p.ID
	create(t, b, inProject)
	inbox := create(t, b, newTodo(u1, time.Second))
	theirs := create(t, b, newTodo(u2, 0))

	if err := b.Todos.DeleteByProject(ctx, u1, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Todos.Get(ctx, u1, inProject.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("DeleteByProject left the project's todo")
	}
	if _, err := b.Todos.Get(ctx, u1, inbox.ID); err != nil {
		t.Fatal("DeleteByProject removed an inbox todo")
	}

	if err := b.Todos.DeleteByUser(ctx, u1); err != nil {
		t.Fatal(err)
	}
	if list, _ := b.Todos.List(ctx, u1, domain.Query{}); len(list) != 0 {
		t.Fatal("DeleteByUser left todos")
	}
	if _, err := b.Todos.Get(ctx, u2, theirs.ID); err != nil {
		t.Fatal("DeleteByUser removed another user's todo")
	}
	if err := b.Todos.DeleteByUser(ctx, u1); err != nil {
		t.Fatalf("deleting nothing must not fail: %v", err)
	}
}

func testConcurrent(t *testing.T, b Backend) {
	user := b.NewUser(t)
	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			todo := newTodo(user, time.Duration(i)*time.Second)
			if err := b.Todos.Create(ctx, todo); err != nil {
				t.Error(err)
				return
			}
			if _, err := b.Todos.Get(ctx, user, todo.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if list, _ := b.Todos.List(ctx, user, domain.Query{}); len(list) != n {
		t.Fatalf("len = %d, want %d", len(list), n)
	}
}

func names[T any](list []*T, name func(*T) string) []string {
	out := []string{}
	for _, v := range list {
		out = append(out, name(v))
	}
	return out
}

func projectNames(t *testing.T, b Backend, userID string, f domain.ProjectFilter) []string {
	t.Helper()
	list, err := b.Projects.List(ctx, userID, f)
	if err != nil {
		t.Fatal(err)
	}
	return names(list, func(p *domain.Project) string { return p.Name })
}

func testProjects(t *testing.T, b Backend) {
	user, other := b.NewUser(t), b.NewUser(t)
	work := newCollection(t, b, user, "Work", 0)
	a := newProject(t, b, user, "A", work.ID, 0)
	newProject(t, b, user, "B", "", 2*time.Second)
	c := newProject(t, b, user, "C", work.ID, time.Second)
	newProject(t, b, other, "Theirs", "", 0)

	got, err := b.Projects.Get(ctx, user, a.ID)
	if err != nil || got.Name != "A" || got.CollectionID != work.ID || got.Archived() || !got.CreatedAt.Equal(a.CreatedAt) {
		t.Fatalf("round trip: %+v (%v)", got, err)
	}

	c.Name, c.Description, c.Icon, c.Color, c.CollectionID = "C2", "desc", "🚀", "blue", ""
	c.ArchivedAt, c.UpdatedAt = at(time.Hour), base.Add(time.Hour)
	if err := b.Projects.Update(ctx, c); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Projects.Get(ctx, user, c.ID); got.Name != "C2" || got.Description != "desc" || got.Icon != "🚀" ||
		got.Color != "blue" || got.CollectionID != "" || !equalTime(got.ArchivedAt, c.ArchivedAt) || !got.UpdatedAt.Equal(c.UpdatedAt) {
		t.Fatalf("update not saved: %+v", got)
	}

	for name, tc := range map[string]struct {
		f    domain.ProjectFilter
		want []string
	}{
		"active, oldest first": {domain.ProjectFilter{}, []string{"A", "B"}},
		"archived":             {domain.ProjectFilter{Archive: domain.ArchivedOnly}, []string{"C2"}},
		"any":                  {domain.ProjectFilter{Archive: domain.AnyArchiveState}, []string{"A", "C2", "B"}},
		"in collection":        {domain.ProjectFilter{CollectionID: work.ID, Archive: domain.AnyArchiveState}, []string{"A"}},
	} {
		if got := projectNames(t, b, user, tc.f); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}

	if err := b.Projects.DetachCollection(ctx, user, work.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Projects.Get(ctx, user, a.ID); got.CollectionID != "" {
		t.Fatal("DetachCollection left the project in the collection")
	}

	expectNotFound(t, map[string]error{
		"Get by another user":    func() error { _, err := b.Projects.Get(ctx, other, a.ID); return err }(),
		"Update by another user": b.Projects.Update(ctx, &domain.Project{ID: a.ID, UserID: other, Meta: domain.Meta{Name: "x"}}),
		"Delete by another user": b.Projects.Delete(ctx, other, a.ID),
		"Get missing":            func() error { _, err := b.Projects.Get(ctx, user, b.MissingID); return err }(),
		"Delete missing":         b.Projects.Delete(ctx, user, b.MissingID),
	})

	if err := b.Projects.Delete(ctx, user, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := b.Projects.DeleteByUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := projectNames(t, b, user, domain.ProjectFilter{Archive: domain.AnyArchiveState}); len(got) != 0 {
		t.Fatalf("DeleteByUser left %q", got)
	}
	if got := projectNames(t, b, other, domain.ProjectFilter{}); len(got) != 1 {
		t.Fatal("DeleteByUser removed another user's project")
	}
}

func testCollections(t *testing.T, b Backend) {
	user, other := b.NewUser(t), b.NewUser(t)
	work := newCollection(t, b, user, "Work", 0)
	home := newCollection(t, b, user, "Home", time.Second)
	newCollection(t, b, other, "Theirs", 0)

	home.Name, home.Icon, home.Color, home.ArchivedAt, home.UpdatedAt = "Home 2", "🏠", "green", at(time.Hour), base.Add(time.Hour)
	if err := b.Collections.Update(ctx, home); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Collections.Get(ctx, user, home.ID); err != nil || got.Name != "Home 2" || got.Icon != "🏠" ||
		got.Color != "green" || !equalTime(got.ArchivedAt, home.ArchivedAt) {
		t.Fatalf("update not saved: %+v (%v)", got, err)
	}

	list := func(f domain.ArchiveFilter) []string {
		l, err := b.Collections.List(ctx, user, f)
		if err != nil {
			t.Fatal(err)
		}
		return names(l, func(c *domain.Collection) string { return c.Name })
	}
	if got := list(domain.ActiveOnly); !slices.Equal(got, []string{"Work"}) {
		t.Errorf("active = %q", got)
	}
	if got := list(domain.ArchivedOnly); !slices.Equal(got, []string{"Home 2"}) {
		t.Errorf("archived = %q", got)
	}
	if got := list(domain.AnyArchiveState); !slices.Equal(got, []string{"Work", "Home 2"}) {
		t.Errorf("any = %q", got)
	}

	expectNotFound(t, map[string]error{
		"Get by another user":    func() error { _, err := b.Collections.Get(ctx, other, work.ID); return err }(),
		"Update by another user": b.Collections.Update(ctx, &domain.Collection{ID: work.ID, UserID: other, Meta: domain.Meta{Name: "x"}}),
		"Delete by another user": b.Collections.Delete(ctx, other, work.ID),
		"Delete missing":         b.Collections.Delete(ctx, user, b.MissingID),
	})

	if err := b.Collections.Delete(ctx, user, work.ID); err != nil {
		t.Fatal(err)
	}
	if err := b.Collections.DeleteByUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := list(domain.AnyArchiveState); len(got) != 0 {
		t.Fatalf("DeleteByUser left %q", got)
	}
}

func testTransactions(t *testing.T, b Backend) {
	user := b.NewUser(t)
	boom := errors.New("boom")
	var projectID string
	err := b.Tx.WithinTx(ctx, func(ctx context.Context) error {
		p := &domain.Project{UserID: user, Meta: domain.Meta{Name: "Tx", Color: "default", CreatedAt: base, UpdatedAt: base}}
		if err := b.Projects.Create(ctx, p); err != nil {
			return err
		}
		projectID = p.ID
		todo := newTodo(user, 0)
		todo.ProjectID = p.ID
		if err := b.Todos.Create(ctx, todo); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the function's error", err)
	}
	if _, err := b.Projects.Get(ctx, user, projectID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("project created in a failed transaction is visible: %v", err)
	}
	if list, _ := b.Todos.List(ctx, user, domain.Query{}); len(list) != 0 {
		t.Fatal("todo created in a failed transaction is visible")
	}
}
