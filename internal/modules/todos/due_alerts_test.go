package todos_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

// fakeNotifier records due-date alerts, like the notifications module's
// unique key would: one per todo, kind and deadline.
type fakeNotifier struct {
	mu     sync.Mutex
	alerts []todos.TodoDueAlert
	fail   error
}

func (*fakeNotifier) TodoCreated(context.Context, todos.NewTeamTodo) error { return nil }

func (n *fakeNotifier) TodoDue(_ context.Context, a todos.TodoDueAlert, _ time.Time) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.fail != nil {
		return n.fail
	}
	n.alerts = append(n.alerts, a)
	return nil
}

// take returns the alerts sent since the last call, as "kind:title".
func (n *fakeNotifier) take() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]string, 0, len(n.alerts))
	for _, a := range n.alerts {
		out = append(out, string(a.Kind)+":"+a.Title)
	}
	n.alerts = nil
	slices.Sort(out)
	return out
}

type dueFixture struct {
	m        *todos.Module
	notifier *fakeNotifier
	now      time.Time
	user     access.Actor
}

func newDueFixture(t *testing.T) *dueFixture {
	t.Helper()
	f := &dueFixture{
		notifier: &fakeNotifier{},
		now:      time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		user:     access.Personal("6650a1b2c3d4e5f6a7b8c9d0"),
	}
	todoStore, projects := memstore.NewTodos(), memstore.NewProjects()
	todoStore.LinkProjects(projects)
	f.m = todos.New(todos.Deps{
		Todos: todoStore, Projects: projects, Collections: memstore.NewCollections(),
		Notifier: f.notifier, Tx: tx.None{}, Now: func() time.Time { return f.now },
	})
	return f
}

func (f *dueFixture) create(t *testing.T, title string, due time.Duration, project string) string {
	t.Helper()
	c := domain.Changes{Title: &title, DueAt: domain.Set(f.now.Add(due))}
	if project != "" {
		c.ProjectID = domain.Set(project)
	}
	id, err := f.m.Commands.CreateTodo.Handle(context.Background(), command.CreateTodo{Actor: f.user, Changes: c})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *dueFixture) update(t *testing.T, id string, c domain.Changes) {
	t.Helper()
	if err := f.m.Commands.UpdateTodo.Handle(context.Background(), command.UpdateTodo{Actor: f.user, ID: id, Changes: c}); err != nil {
		t.Fatal(err)
	}
}

// run runs the job at the fixture's time and returns the alerts it sent.
func (f *dueFixture) run(t *testing.T) []string {
	t.Helper()
	if err := f.m.Commands.SendDueAlerts.Handle(context.Background(), command.SendDueAlerts{}); err != nil {
		t.Fatal(err)
	}
	return f.notifier.take()
}

func expectSent(t *testing.T, step string, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("%s: sent %v, want %v", step, got, want)
	}
}

func TestDueAlerts_SoonThenOverdueOnceEach(t *testing.T) {
	f := newDueFixture(t)
	ship := f.create(t, "Ship it", 30*time.Hour, "")
	f.create(t, "Later", 30*24*time.Hour, "")

	expectSent(t, "more than a day ahead", f.run(t))

	f.now = f.now.Add(7 * time.Hour) // due in 23 hours
	expectSent(t, "within a day", f.run(t), "due_soon:Ship it")
	expectSent(t, "rerun", f.run(t))

	f.now = f.now.Add(23*time.Hour + time.Minute) // a minute past the deadline
	expectSent(t, "past the deadline", f.run(t), "overdue:Ship it")
	f.now = f.now.Add(24 * time.Hour)
	expectSent(t, "a day later", f.run(t))

	// Moving the deadline re-arms both alerts.
	f.update(t, ship, domain.Changes{DueAt: domain.Set(f.now.Add(2 * time.Hour))})
	expectSent(t, "new deadline", f.run(t), "due_soon:Ship it")
	f.now = f.now.Add(3 * time.Hour)
	expectSent(t, "new deadline passed", f.run(t), "overdue:Ship it")
	expectSent(t, "rerun", f.run(t))
}

func TestDueAlerts_FirstSeenOverdueGetsOnlyOverdue(t *testing.T) {
	f := newDueFixture(t)
	f.create(t, "Late", -time.Hour, "")
	expectSent(t, "first run", f.run(t), "overdue:Late")
	expectSent(t, "rerun", f.run(t))
}

func TestDueAlerts_ClosedAndArchivedNeverAlert(t *testing.T) {
	f := newDueFixture(t)
	ctx := context.Background()
	done := f.create(t, "Done", time.Hour, "")
	f.update(t, done, domain.Changes{Status: ptr(domain.StatusDone)})
	cancelled := f.create(t, "Cancelled", -time.Hour, "")
	f.update(t, cancelled, domain.Changes{Status: ptr(domain.StatusCancelled)})

	projectID, err := f.m.Commands.CreateProject.Handle(ctx, command.CreateProject{Actor: f.user, Meta: domain.MetaChanges{Name: ptr("Old")}})
	if err != nil {
		t.Fatal(err)
	}
	f.create(t, "Archived", time.Hour, projectID)
	if err := f.m.Commands.UpdateProject.Handle(ctx, command.UpdateProject{Actor: f.user, ID: projectID, Meta: domain.MetaChanges{Archived: ptr(true)}}); err != nil {
		t.Fatal(err)
	}

	expectSent(t, "due soon", f.run(t))
	f.now = f.now.Add(2 * time.Hour)
	expectSent(t, "overdue", f.run(t))

	// Reopened, the done todo is overdue now.
	f.update(t, done, domain.Changes{Status: ptr(domain.StatusPending)})
	expectSent(t, "reopened", f.run(t), "overdue:Done")
}

func TestDueAlerts_RecipientAndPayload(t *testing.T) {
	f := newDueFixture(t)
	id := f.create(t, "Ship it", time.Hour, "")
	if err := f.m.Commands.SendDueAlerts.Handle(context.Background(), command.SendDueAlerts{}); err != nil {
		t.Fatal(err)
	}
	a := f.notifier.alerts
	if len(a) != 1 {
		t.Fatalf("alerts = %+v", a)
	}
	want := todos.TodoDueAlert{UserID: f.user.UserID, TodoID: id, Title: "Ship it", Kind: domain.AlertDueSoon, DueAt: f.now.Add(time.Hour)}
	if a[0] != want {
		t.Fatalf("alert = %+v, want %+v", a[0], want)
	}
}

func TestDueAlerts_NotifierFailureFailsTheRun(t *testing.T) {
	f := newDueFixture(t)
	f.create(t, "Ship it", time.Hour, "")
	boom := errors.New("boom")
	f.notifier.fail = boom
	err := f.m.Commands.SendDueAlerts.Handle(context.Background(), command.SendDueAlerts{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the notifier's (the job is retried)", err)
	}
}

func TestDueAlerts_BatchesAndCap(t *testing.T) {
	f := newDueFixture(t)
	for i := range 250 { // more than one batch
		f.create(t, fmt.Sprintf("t%03d", i), time.Duration(i+1)*time.Minute, "")
	}
	if got := len(f.run(t)); got != 250 {
		t.Fatalf("sent %d alerts, want 250", got)
	}
	expectSent(t, "rerun", f.run(t))
}

func TestDueAlerts_Registration(t *testing.T) {
	f := newDueFixture(t)
	r := jobs.NewRegistry()
	f.m.RegisterJobs(r)
	if !slices.Contains(r.Kinds(), "todos.due_alerts") {
		t.Fatalf("kinds = %v", r.Kinds())
	}
	s := f.m.Schedules()
	if len(s) != 1 || s[0].Kind != "todos.due_alerts" || s[0].Every != 5*time.Minute {
		t.Fatalf("schedules = %+v", s)
	}
}

func ptr[T any](v T) *T { return &v }
