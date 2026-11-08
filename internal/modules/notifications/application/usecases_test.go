// Use-case tests for the notifications commands and queries, on the memory
// store with a fake team directory and a fake FCM.
package application_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
	jobsmem "github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/memstore"
)

var bg = context.Background()

type directory map[string]access.Roster

func (d directory) Roster(_ context.Context, teamID string) (access.Roster, error) {
	r, ok := d[teamID]
	if !ok {
		return access.Roster{}, access.ErrTeamNotFound
	}
	return r, nil
}

// pusher records pushes; tokens in dead are reported invalid.
type pusher struct {
	mu     sync.Mutex
	pushes []push
	dead   []string
	err    error
}

type push struct {
	tokens []string
	msg    domain.Message
}

func (p *pusher) Push(_ context.Context, tokens []string, m domain.Message) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pushes = append(p.pushes, push{slices.Sorted(slices.Values(tokens)), m})
	var invalid []string
	for _, t := range tokens {
		if slices.Contains(p.dead, t) {
			invalid = append(invalid, t)
		}
	}
	return invalid, p.err
}

type fixture struct {
	module  *notifications.Module
	cmd     command.Handlers
	qry     query.Handlers
	devices *memstore.Devices
	push    *pusher
	clock   time.Time
	jobs    *jobsmem.Store
	runner  *jobs.Runner
}

var team = access.Roster{TeamID: "team-1", Name: "Platform", Members: map[string]access.Role{
	"ann": access.Owner, "bo": access.Admin, "cy": access.Member, "di": access.Viewer,
}}

// newFixture wires the module to an in-memory job queue; notify runs the
// jobs as the worker would.
func newFixture(t *testing.T, withPusher ...bool) *fixture {
	t.Helper()
	f := &fixture{devices: memstore.NewDevices(), push: &pusher{}, clock: time.Now(), jobs: jobsmem.New()}
	var p notifications.Pusher = f.push
	if len(withPusher) > 0 && !withPusher[0] {
		p = nil
	}
	f.module = notifications.New(notifications.Deps{
		Devices: f.devices, Directory: directory{"team-1": team}, Pusher: p,
		Jobs: jobs.NewQueue(f.jobs, nil),
		Now:  func() time.Time { f.clock = f.clock.Add(time.Second); return f.clock },
	})
	registry := jobs.NewRegistry()
	f.module.RegisterJobs(registry)
	f.runner = jobs.NewRunner(f.jobs, registry, jobs.RunnerConfig{}, nil)
	f.cmd, f.qry = f.module.Commands, f.module.Queries
	return f
}

// notify records the event, then runs the jobs it queued.
func (f *fixture) notify(t *testing.T, e notifications.TodoCreated) {
	t.Helper()
	if err := f.module.TodoCreated(bg, e); err != nil {
		t.Fatal(err)
	}
	f.run(t)
}

func (f *fixture) run(t *testing.T) int {
	t.Helper()
	n, err := f.runner.RunDue(bg)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) register(t *testing.T, userID, token string) {
	t.Helper()
	cmd := command.RegisterDevice{UserID: userID, Token: token, Platform: "android"}
	if _, err := f.cmd.RegisterDevice.Handle(bg, cmd); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) tokens(t *testing.T, userID string) []string {
	t.Helper()
	list, err := f.qry.ListDevices.Handle(bg, query.ListDevices{UserID: userID})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, d := range list {
		out = append(out, d.Token)
	}
	return out
}

func newTask(actor string, role access.Role) notifications.TodoCreated {
	return notifications.TodoCreated{
		TeamID:    "team-1",
		ProjectID: "p1",
		TodoID:    "t1",
		Title:     "Ship it",
		ActorID:   actor,
		ActorName: "Bo",
		ActorRole: role,
	}
}

func TestTodoCreated_AdminNotifiesTheRestOfTheTeam(t *testing.T) {
	f := newFixture(t)
	devices := map[string]string{
		"ann":      "ann-phone",
		"bo":       "bo-phone",
		"cy":       "cy-phone",
		"di":       "di-web",
		"outsider": "out-phone",
	}
	for user, tok := range devices {
		f.register(t, user, tok)
	}
	f.register(t, "cy", "cy-tablet")

	f.notify(t, newTask("bo", access.Admin))
	if len(f.push.pushes) != 1 {
		t.Fatalf("pushes = %d, want 1", len(f.push.pushes))
	}
	got := f.push.pushes[0]
	if !slices.Equal(got.tokens, []string{"ann-phone", "cy-phone", "cy-tablet", "di-web"}) {
		t.Fatalf("pushed to %v; want the team's other members, not the admin or outsiders", got.tokens)
	}
	if got.msg.Title != "New task in Platform" || got.msg.Data["todoId"] != "t1" {
		t.Fatalf("message = %+v", got.msg)
	}
}

func TestTodoCreated_WhoTriggersIt(t *testing.T) {
	f := newFixture(t)
	f.register(t, "cy", "cy-phone")
	f.register(t, "bo", "bo-phone")

	f.notify(t, newTask("cy", access.Member))
	personal := newTask("ann", access.Owner)
	personal.TeamID = ""
	f.notify(t, personal)
	if len(f.push.pushes) != 0 {
		t.Fatalf("members' and personal tasks must not notify: %+v", f.push.pushes)
	}
	f.notify(t, newTask("ann", access.Owner))
	if len(f.push.pushes) != 1 {
		t.Fatal("the owner's task must notify")
	}
}

func TestTodoCreated_ForgetsInvalidTokensAndSurvivesFailures(t *testing.T) {
	f := newFixture(t)
	f.register(t, "cy", "cy-old")
	f.register(t, "cy", "cy-new")
	f.push.dead = []string{"cy-old"}
	f.push.err = errors.New("one send failed")

	// Failures are logged, never returned to the caller.
	f.notify(t, newTask("bo", access.Admin))
	if got := f.tokens(t, "cy"); !slices.Equal(got, []string{"cy-new"}) {
		t.Fatalf("devices after push = %v, want the invalid token forgotten", got)
	}
}

func TestTodoCreated_NothingToDo(t *testing.T) {
	f := newFixture(t)
	gone := newTask("bo", access.Admin)
	gone.TeamID = "deleted-team"
	if err := f.cmd.NotifyTodoCreated.Handle(bg, command.NotifyTodoCreated{Event: gone}); err != nil {
		t.Fatalf("a team deleted in the meantime: %v", err)
	}
	task := command.NotifyTodoCreated{Event: newTask("bo", access.Admin)}
	if err := f.cmd.NotifyTodoCreated.Handle(bg, task); err != nil {
		t.Fatalf("no devices: %v", err)
	}
	if len(f.push.pushes) != 0 {
		t.Fatalf("pushed without recipients: %+v", f.push.pushes)
	}

	off := newFixture(t, false)
	off.register(t, "cy", "cy-phone")
	if err := off.cmd.NotifyTodoCreated.Handle(bg, task); err != nil {
		t.Fatalf("without a pusher: %v", err)
	}
}

func TestTodoCreated_OnlyEnqueues(t *testing.T) {
	f := newFixture(t)
	f.register(t, "cy", "cy-phone")
	if err := f.module.TodoCreated(bg, newTask("bo", access.Admin)); err != nil {
		t.Fatal(err)
	}
	if len(f.push.pushes) != 0 {
		t.Fatal("the request must only record the notification; the worker sends it")
	}
	if n := f.run(t); n != 1 || len(f.push.pushes) != 1 {
		t.Fatalf("ran %d jobs, %d pushes; want the notification sent by its job", n, len(f.push.pushes))
	}
	if err := f.module.TodoCreated(bg, newTask("cy", access.Member)); err != nil || f.run(t) != 0 {
		t.Fatal("a member's task must not even queue a job")
	}
}

func TestPushToUsers(t *testing.T) {
	f := newFixture(t)
	f.register(t, "ann", "ann-phone")
	f.register(t, "bo", "bo-phone")
	p := notifications.Push{UserIDs: []string{"ann"}, Message: domain.Message{Title: "Due soon", Data: map[string]string{"todoId": "t1"}}}
	for range 2 {
		if err := f.module.PushToUsers(bg, p, "due-soon:t1"); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.run(t); n != 1 {
		t.Fatalf("ran %d jobs, want one: the key sends it once", n)
	}
	if len(f.push.pushes) != 1 || !slices.Equal(f.push.pushes[0].tokens, []string{"ann-phone"}) || f.push.pushes[0].msg.Title != "Due soon" {
		t.Fatalf("pushes = %+v", f.push.pushes)
	}
}

func TestDeliveryFailuresAreNotRetried(t *testing.T) {
	f := newFixture(t)
	f.register(t, "cy", "cy-phone")
	f.push.err = errors.New("FCM unavailable for some devices")
	f.notify(t, newTask("bo", access.Admin))
	c, _ := f.jobs.Counts(bg, time.Time{})
	if c.ByStatus[jobs.Succeeded] != 1 || c.ByStatus[jobs.Queued] != 0 {
		t.Fatalf("jobs = %+v: a partly failed push must not be retried (devices that got it would get it twice)", c.ByStatus)
	}
}

func TestRegisterDevice(t *testing.T) {
	f := newFixture(t)
	empty := command.RegisterDevice{UserID: "ann", Token: "", Platform: "web"}
	if _, err := f.cmd.RegisterDevice.Handle(bg, empty); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("empty token: err = %v", err)
	}
	f.register(t, "ann", "shared")
	f.register(t, "bo", "shared") // Bo signed in on Ann's old phone
	if got := f.tokens(t, "ann"); len(got) != 0 {
		t.Fatalf("token still with Ann: %v", got)
	}

	for i := range domain.MaxDevicesPerUser + 2 {
		f.register(t, "cy", "cy-"+string(rune('a'+i)))
	}
	got := f.tokens(t, "cy")
	if len(got) != domain.MaxDevicesPerUser || got[0] != "cy-c" {
		t.Fatalf("devices = %v; want the %d most recent", got, domain.MaxDevicesPerUser)
	}
}

func TestUnregisterAndDeleteDevices(t *testing.T) {
	f := newFixture(t)
	f.register(t, "ann", "ann-phone")
	f.register(t, "ann", "ann-web")
	phone := domain.DeviceID("ann-phone")
	othersDevice := command.UnregisterDevice{UserID: "bo", DeviceID: phone}
	if err := f.cmd.UnregisterDevice.Handle(bg, othersDevice); !errors.Is(err, domain.ErrDeviceNotFound) {
		t.Fatalf("another user's device: err = %v", err)
	}
	byToken := command.UnregisterDevice{UserID: "ann", DeviceID: "ann-phone"}
	if err := f.cmd.UnregisterDevice.Handle(bg, byToken); !errors.Is(err, domain.ErrDeviceNotFound) {
		t.Fatalf("a token is not a device ID: err = %v", err)
	}
	ownDevice := command.UnregisterDevice{UserID: "ann", DeviceID: phone}
	if err := f.cmd.UnregisterDevice.Handle(bg, ownDevice); err != nil {
		t.Fatal(err)
	}
	if got := f.tokens(t, "ann"); !slices.Equal(got, []string{"ann-web"}) {
		t.Fatalf("devices after unregistering the phone: %v", got)
	}
	if err := f.module.DeleteUserDevices(bg, "ann"); err != nil {
		t.Fatal(err)
	}
	if got := f.tokens(t, "ann"); len(got) != 0 {
		t.Fatalf("devices left: %v", got)
	}
}
