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
}

var team = access.Roster{TeamID: "team-1", Name: "Platform", Members: map[string]access.Role{
	"ann": access.Owner, "bo": access.Admin, "cy": access.Member, "di": access.Viewer,
}}

// newFixture runs notifications in place (no goroutine), so tests see them.
func newFixture(t *testing.T, withPusher ...bool) *fixture {
	t.Helper()
	f := &fixture{devices: memstore.NewDevices(), push: &pusher{}, clock: time.Now()}
	var p notifications.Pusher = f.push
	if len(withPusher) > 0 && !withPusher[0] {
		p = nil
	}
	f.module = notifications.New(notifications.Deps{
		Devices: f.devices, Directory: directory{"team-1": team}, Pusher: p,
		Dispatch: func(fn func()) { fn() },
		Now:      func() time.Time { f.clock = f.clock.Add(time.Second); return f.clock },
	})
	f.cmd, f.qry = f.module.Commands, f.module.Queries
	return f
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

	f.module.TodoCreated(bg, newTask("bo", access.Admin))
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

	f.module.TodoCreated(bg, newTask("cy", access.Member))
	personal := newTask("ann", access.Owner)
	personal.TeamID = ""
	f.module.TodoCreated(bg, personal)
	if len(f.push.pushes) != 0 {
		t.Fatalf("members' and personal tasks must not notify: %+v", f.push.pushes)
	}
	f.module.TodoCreated(bg, newTask("ann", access.Owner))
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
	f.module.TodoCreated(bg, newTask("bo", access.Admin))
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

func TestTodoCreated_RunsAfterTheRequest(t *testing.T) {
	f := newFixture(t)
	f.register(t, "cy", "cy-phone")
	var queued func()
	m := notifications.New(notifications.Deps{
		Devices: f.devices, Directory: directory{"team-1": team}, Pusher: f.push,
		Dispatch: func(fn func()) { queued = fn },
	})
	ctx, cancel := context.WithCancel(bg)
	m.TodoCreated(ctx, newTask("bo", access.Admin))
	cancel() // the request is over
	if queued == nil || len(f.push.pushes) != 0 {
		t.Fatal("the notification must be dispatched, not sent in the request")
	}
	queued()
	if len(f.push.pushes) != 1 {
		t.Fatal("a finished request must not cancel its notification")
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
