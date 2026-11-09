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
	// NewUser returns a fresh, well-formed user ID for this backend.
	NewUser func(t *testing.T) string
	// NewID returns a fresh, well-formed ID (used for teams).
	NewID func() string
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
	t.Run("TeamScope", func(t *testing.T) { testTeamScope(t, newBackend(t)) })
	t.Run("TeamBulkOperations", func(t *testing.T) { testTeamBulk(t, newBackend(t)) })
	t.Run("DueAlertsListing", func(t *testing.T) { testDueAlertsListing(t, newBackend(t)) })
	t.Run("DueAlertsMarking", func(t *testing.T) { testDueAlertsMarking(t, newBackend(t)) })
	t.Run("Transactions", func(t *testing.T) {
		b := newBackend(t)
		if !b.AtomicTx {
			t.Skip("this backend runs without transactions")
		}
		testTransactions(t, b)
	})
}

var ctx = context.Background()

// me is the scope of a user without teams.
func me(userID string) domain.Scope { return domain.Scope{UserID: userID} }

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

	got, err := b.Todos.Get(ctx, me(owner), todo.ID)
	if err != nil || !sameTodo(got, todo) {
		t.Fatalf("round trip:\n got  %+v (%v)\n want %+v", got, err, todo)
	}

	plain := create(t, b, newTodo(owner, time.Minute))
	if got, _ := b.Todos.Get(ctx, me(owner), plain.ID); got.ProjectID != "" || got.DueAt != nil || got.Tags == nil || len(got.Tags) != 0 {
		t.Fatalf("empty optional fields must round-trip as empty: %+v", got)
	}

	expectNotFound(t, map[string]error{
		"Get by another user":    func() error { _, err := b.Todos.Get(ctx, me(other), todo.ID); return err }(),
		"Update by another user": b.Todos.Update(ctx, me(other), &domain.Todo{ID: todo.ID, UserID: other, Title: "x", Status: domain.StatusPending}),
		"Delete by another user": b.Todos.Delete(ctx, me(other), todo.ID),
		"Get missing":            func() error { _, err := b.Todos.Get(ctx, me(owner), b.MissingID); return err }(),
		"Update missing":         b.Todos.Update(ctx, me(owner), &domain.Todo{ID: b.MissingID, UserID: owner, Title: "x", Status: domain.StatusPending}),
		"Delete missing":         b.Todos.Delete(ctx, me(owner), b.MissingID),
	})

	// Update saves every field, including clearing optional ones.
	todo.ProjectID, todo.Title, todo.Notes, todo.Status = "", "changed", "", domain.StatusInProgress
	todo.Priority, todo.Tags, todo.DueAt, todo.CompletedAt, todo.UpdatedAt = domain.PriorityNone, []string{}, nil, nil, base.Add(time.Hour)
	if err := b.Todos.Update(ctx, me(todo.UserID), todo); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Todos.Get(ctx, me(owner), todo.ID); !sameTodo(got, todo) {
		t.Fatalf("update:\n got  %+v\n want %+v", got, todo)
	}

	if err := b.Todos.Delete(ctx, me(owner), todo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Todos.Get(ctx, me(owner), todo.ID); !errors.Is(err, apperrors.ErrNotFound) {
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
		updated  int // minutes after base + 1h
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
		todo.UpdatedAt = base.Add(time.Hour + time.Duration(s.updated)*time.Minute)
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
			list, err := b.Todos.List(ctx, me(user), q)
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

	got, err := b.Todos.Progress(ctx, me(user), base)
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

	if err := b.Todos.DeleteByProject(ctx, me(u1), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Todos.Get(ctx, me(u1), inProject.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("DeleteByProject left the project's todo")
	}
	if _, err := b.Todos.Get(ctx, me(u1), inbox.ID); err != nil {
		t.Fatal("DeleteByProject removed an inbox todo")
	}

	if err := b.Todos.DeleteByUser(ctx, u1); err != nil {
		t.Fatal(err)
	}
	if list, _ := b.Todos.List(ctx, me(u1), domain.Query{}); len(list) != 0 {
		t.Fatal("DeleteByUser left todos")
	}
	if _, err := b.Todos.Get(ctx, me(u2), theirs.ID); err != nil {
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
			if _, err := b.Todos.Get(ctx, me(user), todo.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if list, _ := b.Todos.List(ctx, me(user), domain.Query{}); len(list) != n {
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

func projectNames(t *testing.T, b Backend, sc domain.Scope, f domain.ProjectFilter) []string {
	t.Helper()
	list, err := b.Projects.List(ctx, sc, f)
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

	got, err := b.Projects.Get(ctx, me(user), a.ID)
	if err != nil || got.Name != "A" || got.CollectionID != work.ID || got.Archived() || !got.CreatedAt.Equal(a.CreatedAt) {
		t.Fatalf("round trip: %+v (%v)", got, err)
	}

	c.Name, c.Description, c.Icon, c.Color, c.CollectionID = "C2", "desc", "🚀", "blue", ""
	c.ArchivedAt, c.UpdatedAt = at(time.Hour), base.Add(time.Hour)
	if err := b.Projects.Update(ctx, me(c.UserID), c); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Projects.Get(ctx, me(user), c.ID); got.Name != "C2" || got.Description != "desc" || got.Icon != "🚀" ||
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
		if got := projectNames(t, b, me(user), tc.f); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}

	if err := b.Projects.DetachCollection(ctx, me(user), work.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Projects.Get(ctx, me(user), a.ID); got.CollectionID != "" {
		t.Fatal("DetachCollection left the project in the collection")
	}

	expectNotFound(t, map[string]error{
		"Get by another user":    func() error { _, err := b.Projects.Get(ctx, me(other), a.ID); return err }(),
		"Update by another user": b.Projects.Update(ctx, me(other), &domain.Project{ID: a.ID, UserID: other, Meta: domain.Meta{Name: "x"}}),
		"Delete by another user": b.Projects.Delete(ctx, me(other), a.ID),
		"Get missing":            func() error { _, err := b.Projects.Get(ctx, me(user), b.MissingID); return err }(),
		"Delete missing":         b.Projects.Delete(ctx, me(user), b.MissingID),
	})

	if err := b.Projects.Delete(ctx, me(user), a.ID); err != nil {
		t.Fatal(err)
	}
	if err := b.Projects.DeleteByUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := projectNames(t, b, me(user), domain.ProjectFilter{Archive: domain.AnyArchiveState}); len(got) != 0 {
		t.Fatalf("DeleteByUser left %q", got)
	}
	if got := projectNames(t, b, me(other), domain.ProjectFilter{}); len(got) != 1 {
		t.Fatal("DeleteByUser removed another user's project")
	}
}

func testCollections(t *testing.T, b Backend) {
	user, other := b.NewUser(t), b.NewUser(t)
	work := newCollection(t, b, user, "Work", 0)
	home := newCollection(t, b, user, "Home", time.Second)
	newCollection(t, b, other, "Theirs", 0)

	home.Name, home.Icon, home.Color, home.ArchivedAt, home.UpdatedAt = "Home 2", "🏠", "green", at(time.Hour), base.Add(time.Hour)
	if err := b.Collections.Update(ctx, me(home.UserID), home); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Collections.Get(ctx, me(user), home.ID); err != nil || got.Name != "Home 2" || got.Icon != "🏠" ||
		got.Color != "green" || !equalTime(got.ArchivedAt, home.ArchivedAt) {
		t.Fatalf("update not saved: %+v (%v)", got, err)
	}

	list := func(f domain.ArchiveFilter) []string {
		l, err := b.Collections.List(ctx, me(user), domain.CollectionFilter{Archive: f})
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
		"Get by another user":    func() error { _, err := b.Collections.Get(ctx, me(other), work.ID); return err }(),
		"Update by another user": b.Collections.Update(ctx, me(other), &domain.Collection{ID: work.ID, UserID: other, Meta: domain.Meta{Name: "x"}}),
		"Delete by another user": b.Collections.Delete(ctx, me(other), work.ID),
		"Delete missing":         b.Collections.Delete(ctx, me(user), b.MissingID),
	})

	if err := b.Collections.Delete(ctx, me(user), work.ID); err != nil {
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
	if _, err := b.Projects.Get(ctx, me(user), projectID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("project created in a failed transaction is visible: %v", err)
	}
	if list, _ := b.Todos.List(ctx, me(user), domain.Query{}); len(list) != 0 {
		t.Fatal("todo created in a failed transaction is visible")
	}
}

// teamFixture is a team with a project, a collection and a todo created by
// creator, plus creator's personal todo.
type teamFixture struct {
	team, creator, outsider string
	project                 *domain.Project
	collection              *domain.Collection
	todo, personal          *domain.Todo
}

func newTeamFixture(t *testing.T, b Backend) teamFixture {
	t.Helper()
	f := teamFixture{team: b.NewID(), creator: b.NewUser(t), outsider: b.NewUser(t)}
	f.collection = &domain.Collection{UserID: f.creator, TeamID: f.team, Meta: domain.Meta{Name: "Team C", Color: "default", CreatedAt: base, UpdatedAt: base}}
	if err := b.Collections.Create(ctx, f.collection); err != nil {
		t.Fatal(err)
	}
	f.project = &domain.Project{UserID: f.creator, TeamID: f.team, CollectionID: f.collection.ID, Meta: domain.Meta{Name: "Team P", Color: "default", CreatedAt: base, UpdatedAt: base}}
	if err := b.Projects.Create(ctx, f.project); err != nil {
		t.Fatal(err)
	}
	f.todo = newTodo(f.creator, 0)
	f.todo.TeamID, f.todo.ProjectID, f.todo.Title = f.team, f.project.ID, "team todo"
	create(t, b, f.todo)
	f.personal = newTodo(f.creator, time.Second)
	f.personal.Title = "personal todo"
	create(t, b, f.personal)
	return f
}

func (f teamFixture) member(userID string) domain.Scope {
	return domain.Scope{UserID: userID, TeamIDs: []string{f.team}}
}

func testTeamScope(t *testing.T, b Backend) {
	f := newTeamFixture(t, b)
	colleague := b.NewUser(t)
	inTeam, outside := f.member(colleague), me(f.outsider)

	// Team records round-trip their team and are visible to every member...
	if got, err := b.Todos.Get(ctx, inTeam, f.todo.ID); err != nil || got.TeamID != f.team || got.UserID != f.creator {
		t.Fatalf("team todo for a member: %+v (%v)", got, err)
	}
	if got, err := b.Projects.Get(ctx, inTeam, f.project.ID); err != nil || got.TeamID != f.team {
		t.Fatalf("team project for a member: %+v (%v)", got, err)
	}
	if got, err := b.Collections.Get(ctx, inTeam, f.collection.ID); err != nil || got.TeamID != f.team {
		t.Fatalf("team collection for a member: %+v (%v)", got, err)
	}
	// ...but not to outsiders, nor to their creator without the team in scope,
	// and a member never sees a colleague's personal records.
	expectNotFound(t, map[string]error{
		"todo for outsider":        func() error { _, err := b.Todos.Get(ctx, outside, f.todo.ID); return err }(),
		"todo for creator alone":   func() error { _, err := b.Todos.Get(ctx, me(f.creator), f.todo.ID); return err }(),
		"project for outsider":     func() error { _, err := b.Projects.Get(ctx, outside, f.project.ID); return err }(),
		"collection for outsider":  func() error { _, err := b.Collections.Get(ctx, outside, f.collection.ID); return err }(),
		"colleague's personal":     func() error { _, err := b.Todos.Get(ctx, inTeam, f.personal.ID); return err }(),
		"update by outsider":       b.Todos.Update(ctx, outside, f.todo),
		"delete by outsider":       b.Todos.Delete(ctx, outside, f.todo.ID),
		"delete project outsider":  b.Projects.Delete(ctx, outside, f.project.ID),
		"update coll. by outsider": b.Collections.Update(ctx, outside, f.collection),
	})

	// Listings combine personal and team records; the owner filter narrows them.
	both := f.member(f.creator)
	for name, tc := range map[string]struct {
		q    domain.Query
		want []string
	}{
		"all in scope":  {domain.Query{}, []string{"personal todo", "team todo"}},
		"personal only": {domain.Query{OwnerFilter: domain.OwnerFilter{Personal: true}}, []string{"personal todo"}},
		"one team":      {domain.Query{OwnerFilter: domain.OwnerFilter{TeamID: f.team}}, []string{"team todo"}},
	} {
		list, err := b.Todos.List(ctx, both, tc.q)
		if err != nil {
			t.Fatal(err)
		}
		if got := names(list, func(t *domain.Todo) string { return t.Title }); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
	newProject(t, b, f.creator, "Mine", "", time.Second)
	if got := projectNames(t, b, both, domain.ProjectFilter{OwnerFilter: domain.OwnerFilter{TeamID: f.team}}); !slices.Equal(got, []string{"Team P"}) {
		t.Errorf("team projects = %q", got)
	}
	if got := projectNames(t, b, both, domain.ProjectFilter{OwnerFilter: domain.OwnerFilter{Personal: true}}); !slices.Equal(got, []string{"Mine"}) {
		t.Errorf("personal projects = %q", got)
	}
	if got := projectNames(t, b, inTeam, domain.ProjectFilter{}); !slices.Equal(got, []string{"Team P"}) {
		t.Errorf("a colleague sees %q", got)
	}
	colls, err := b.Collections.List(ctx, inTeam, domain.CollectionFilter{OwnerFilter: domain.OwnerFilter{TeamID: f.team}})
	if err != nil || len(colls) != 1 || colls[0].ID != f.collection.ID {
		t.Fatalf("team collections = %v (%v)", colls, err)
	}
	if colls, _ := b.Collections.List(ctx, inTeam, domain.CollectionFilter{OwnerFilter: domain.OwnerFilter{Personal: true}}); len(colls) != 0 {
		t.Fatalf("a colleague has no personal collections: %v", colls)
	}

	// Progress counts team todos for members.
	progress, err := b.Todos.Progress(ctx, inTeam, base)
	if err != nil || progress[f.project.ID].Total != 1 || progress[""].Total != 0 {
		t.Fatalf("progress for a member = %v (%v)", progress, err)
	}

	// A member may update a team todo; Update saves team and creator, so a
	// todo can move out of the team into the member's personal inbox.
	moved := *f.todo
	moved.TeamID, moved.ProjectID, moved.UserID = "", "", colleague
	if err := b.Todos.Update(ctx, inTeam, &moved); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Todos.Get(ctx, me(f.creator), f.todo.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("moved todo still visible to its creator: %v", err)
	}
	if got, err := b.Todos.Get(ctx, me(colleague), f.todo.ID); err != nil || got.TeamID != "" || got.UserID != colleague {
		t.Fatalf("moved todo is now the colleague's personal todo: %+v (%v)", got, err)
	}
}

func testTeamBulk(t *testing.T, b Backend) {
	f := newTeamFixture(t, b)
	other := newTeamFixture(t, b)
	scope := f.member(f.creator)

	if err := b.Projects.DetachCollection(ctx, me(f.creator), f.collection.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Projects.Get(ctx, scope, f.project.ID); got.CollectionID != f.collection.ID {
		t.Fatal("DetachCollection reached a team project outside the scope")
	}
	if err := b.Todos.DeleteByProject(ctx, me(f.creator), f.project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Todos.Get(ctx, scope, f.todo.ID); err != nil {
		t.Fatal("DeleteByProject reached a team todo outside the scope")
	}

	// DeleteByUser removes only personal records: team records stay with the team.
	if err := b.Todos.DeleteByUser(ctx, f.creator); err != nil {
		t.Fatal(err)
	}
	if err := b.Projects.DeleteByUser(ctx, f.creator); err != nil {
		t.Fatal(err)
	}
	if err := b.Collections.DeleteByUser(ctx, f.creator); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Todos.Get(ctx, me(f.creator), f.personal.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("DeleteByUser left a personal todo")
	}
	if _, err := b.Todos.Get(ctx, scope, f.todo.ID); err != nil {
		t.Fatalf("DeleteByUser removed a team todo: %v", err)
	}
	if _, err := b.Projects.Get(ctx, scope, f.project.ID); err != nil {
		t.Fatalf("DeleteByUser removed a team project: %v", err)
	}

	// DeleteByTeam removes that team's records only.
	for _, del := range []func(context.Context, string) error{b.Todos.DeleteByTeam, b.Projects.DeleteByTeam, b.Collections.DeleteByTeam} {
		if err := del(ctx, f.team); err != nil {
			t.Fatal(err)
		}
	}
	expectNotFound(t, map[string]error{
		"team todo":       func() error { _, err := b.Todos.Get(ctx, scope, f.todo.ID); return err }(),
		"team project":    func() error { _, err := b.Projects.Get(ctx, scope, f.project.ID); return err }(),
		"team collection": func() error { _, err := b.Collections.Get(ctx, scope, f.collection.ID); return err }(),
	})
	otherScope := other.member(other.creator)
	if _, err := b.Todos.Get(ctx, otherScope, other.todo.ID); err != nil {
		t.Fatalf("DeleteByTeam removed another team's todo: %v", err)
	}
	if _, err := b.Collections.Get(ctx, otherScope, other.collection.ID); err != nil {
		t.Fatalf("DeleteByTeam removed another team's collection: %v", err)
	}
}

// ---- due-date alerts (system-level) ----

// dueTodo creates an open todo of user due at now+due.
func dueTodo(t *testing.T, b Backend, user string, now time.Time, due time.Duration) *domain.Todo {
	t.Helper()
	todo := newTodo(user, 0)
	d := now.Add(due)
	todo.DueAt = &d
	return create(t, b, todo)
}

// pendingAlerts lists every pending alert at now as todo ID -> kind, and
// checks the order (earliest deadline first, then ID).
func pendingAlerts(t *testing.T, b Backend, now time.Time) map[string]domain.DueAlertKind {
	t.Helper()
	list, err := b.Todos.ListPendingDueAlertsSystemWide(ctx, now, 1000)
	if err != nil {
		t.Fatalf("list pending due alerts: %v", err)
	}
	out := map[string]domain.DueAlertKind{}
	for i, a := range list {
		if i > 0 {
			prev := list[i-1].Todo
			if c := prev.DueAt.Compare(*a.Todo.DueAt); c > 0 || c == 0 && prev.ID > a.Todo.ID {
				t.Fatalf("alerts out of order: %v before %v", prev.DueAt, a.Todo.DueAt)
			}
		}
		out[a.Todo.ID] = a.Kind
	}
	return out
}

func expectAlerts(t *testing.T, got map[string]domain.DueAlertKind, want map[string]domain.DueAlertKind) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("pending alerts = %v, want %v", got, want)
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Fatalf("pending alerts = %v, want %v", got, want)
		}
	}
}

func mark(t *testing.T, b Backend, todo *domain.Todo, kind domain.DueAlertKind) bool {
	t.Helper()
	ok, err := b.Todos.MarkDueAlerted(ctx, todo.ID, kind, *todo.DueAt)
	if err != nil {
		t.Fatalf("mark %s: %v", kind, err)
	}
	return ok
}

func testDueAlertsListing(t *testing.T, b Backend) {
	owner, other := b.NewUser(t), b.NewUser(t)
	now := base.Add(240 * time.Hour)

	soon := dueTodo(t, b, owner, now, time.Hour)
	overdue := dueTodo(t, b, other, now, -time.Hour)
	edge := dueTodo(t, b, owner, now, domain.DueSoonWindow) // the window includes its end
	dueTodo(t, b, owner, now, domain.DueSoonWindow+time.Millisecond)
	create(t, b, newTodo(owner, 0)) // no deadline

	team := newTodo(owner, 0)
	team.TeamID = b.NewID()
	team.DueAt = at(240*time.Hour + 2*time.Hour)
	create(t, b, team)

	for _, st := range []domain.Status{domain.StatusDone, domain.StatusCancelled} {
		closed := newTodo(owner, 0)
		closed.Status, closed.DueAt = st, at(240*time.Hour-time.Hour)
		create(t, b, closed)
	}
	inBlocked := dueTodo(t, b, owner, now, 3*time.Hour)
	inBlocked.Status = domain.StatusBlocked
	if err := b.Todos.Update(ctx, me(owner), inBlocked); err != nil {
		t.Fatal(err)
	}

	active := newProject(t, b, owner, "Active", "", 0)
	inActive := newTodo(owner, 0)
	inActive.ProjectID, inActive.DueAt = active.ID, at(240*time.Hour-2*time.Hour)
	create(t, b, inActive)
	archived := newProject(t, b, owner, "Archived", "", 0)
	archived.ArchivedAt = at(time.Hour)
	if err := b.Projects.Update(ctx, me(owner), archived); err != nil {
		t.Fatal(err)
	}
	inArchived := newTodo(owner, 0)
	inArchived.ProjectID, inArchived.DueAt = archived.ID, at(240*time.Hour-time.Hour)
	create(t, b, inArchived)

	want := map[string]domain.DueAlertKind{
		soon.ID: domain.AlertDueSoon, edge.ID: domain.AlertDueSoon, team.ID: domain.AlertDueSoon,
		inBlocked.ID: domain.AlertDueSoon, overdue.ID: domain.AlertOverdue, inActive.ID: domain.AlertOverdue,
	}
	expectAlerts(t, pendingAlerts(t, b, now), want)

	// Bounded batches, earliest deadline first, todos fully read.
	first, err := b.Todos.ListPendingDueAlertsSystemWide(ctx, now, 2)
	if err != nil || len(first) != 2 || first[0].Todo.ID != inActive.ID || first[1].Todo.ID != overdue.ID {
		t.Fatalf("first batch of 2 = %+v (%v), want the two overdue todos", first, err)
	}
	if !sameTodo(first[0].Todo, inActive) || first[0].Todo.TeamID != "" {
		t.Fatalf("listed todo = %+v, want %+v", first[0].Todo, inActive)
	}
	if none, err := b.Todos.ListPendingDueAlertsSystemWide(ctx, now, 0); err != nil || len(none) != 0 {
		t.Fatalf("limit 0 = %v (%v)", none, err)
	}

	// Alerted for this deadline: left out, even after an unrelated update.
	if !mark(t, b, soon, domain.AlertDueSoon) || !mark(t, b, overdue, domain.AlertOverdue) {
		t.Fatal("marking pending alerts must succeed")
	}
	soon.Title, soon.UpdatedAt = "renamed", soon.UpdatedAt.Add(time.Minute)
	if err := b.Todos.Update(ctx, me(owner), soon); err != nil {
		t.Fatal(err)
	}
	delete(want, soon.ID)
	delete(want, overdue.ID)
	expectAlerts(t, pendingAlerts(t, b, now), want)

	// Due soon was sent; once the deadline passes, overdue is still due.
	later := soon.DueAt.Add(time.Minute)
	if got := pendingAlerts(t, b, later); got[soon.ID] != domain.AlertOverdue {
		t.Fatalf("after the deadline: %v, want %s overdue", got, soon.ID)
	}
	// Overdue was sent for this deadline: nothing more for it, ever.
	if got := pendingAlerts(t, b, now.Add(48*time.Hour)); got[overdue.ID] != "" {
		t.Fatalf("overdue sent: %v, want %s left out", got, overdue.ID)
	}

	// Alerted for an older deadline: a new deadline re-arms the alerts.
	soon.DueAt = at(240*time.Hour + 3*time.Hour)
	overdue.DueAt = at(240*time.Hour - 2*time.Hour)
	for user, todo := range map[string]*domain.Todo{owner: soon, other: overdue} {
		if err := b.Todos.Update(ctx, me(user), todo); err != nil {
			t.Fatal(err)
		}
	}
	want[soon.ID], want[overdue.ID] = domain.AlertDueSoon, domain.AlertOverdue
	expectAlerts(t, pendingAlerts(t, b, now), want)

	// Restoring the archived project brings its todos back.
	archived.ArchivedAt = nil
	if err := b.Projects.Update(ctx, me(owner), archived); err != nil {
		t.Fatal(err)
	}
	want[inArchived.ID] = domain.AlertOverdue
	expectAlerts(t, pendingAlerts(t, b, now), want)
}

func testDueAlertsMarking(t *testing.T, b Backend) {
	user := b.NewUser(t)
	now := base.Add(240 * time.Hour)
	todo := dueTodo(t, b, user, now, time.Hour)
	due := *todo.DueAt

	markAt := func(id string, kind domain.DueAlertKind, dueAt time.Time) bool {
		t.Helper()
		ok, err := b.Todos.MarkDueAlerted(ctx, id, kind, dueAt)
		if err != nil {
			t.Fatalf("mark %s: %v", kind, err)
		}
		return ok
	}
	// Only for the todo's current deadline, and only for a todo that exists.
	if markAt(todo.ID, domain.AlertDueSoon, due.Add(time.Minute)) || markAt(b.MissingID, domain.AlertDueSoon, due) {
		t.Fatal("marking another deadline or a missing todo must report false")
	}
	if !markAt(todo.ID, domain.AlertDueSoon, due) {
		t.Fatal("first mark must succeed")
	}
	if markAt(todo.ID, domain.AlertDueSoon, due) {
		t.Fatal("marking twice for the same deadline must report false")
	}
	// The kinds are independent.
	if !markAt(todo.ID, domain.AlertOverdue, due) || markAt(todo.ID, domain.AlertOverdue, due) {
		t.Fatal("overdue is marked once, independently of due soon")
	}

	// A closed todo is never marked.
	done := dueTodo(t, b, user, now, -time.Hour)
	done.Status, done.CompletedAt = domain.StatusDone, at(time.Hour)
	if err := b.Todos.Update(ctx, me(user), done); err != nil {
		t.Fatal(err)
	}
	if markAt(done.ID, domain.AlertOverdue, *done.DueAt) {
		t.Fatal("a done todo must not be marked")
	}

	// Concurrent runs: exactly one marks.
	contested := dueTodo(t, b, user, now, -time.Minute)
	const n = 8
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		marked int
	)
	for range n {
		wg.Go(func() {
			ok, err := b.Todos.MarkDueAlerted(ctx, contested.ID, domain.AlertOverdue, *contested.DueAt)
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				mu.Lock()
				marked++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if marked != 1 {
		t.Fatalf("%d concurrent marks succeeded, want exactly 1", marked)
	}
}
