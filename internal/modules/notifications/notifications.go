// Package notifications is the notifications module's public surface: the
// devices users receive push notifications on (Firebase Cloud Messaging) and
// the notifications other modules trigger. Today that is one event: the
// owner or an admin of a team adds a task, and the other members are told.
//
// The module depends only on platform: team rosters come through
// access.Directory (implemented by the teams module), so todos can call it
// without a cycle.
//
// Layout (CQRS inside hexagonal layers):
//
//	domain/                devices, who an event notifies, messages
//	application/           ports
//	application/command/   register devices, notify
//	application/query/     list devices
//	transport/             driving adapter (HTTP)
//	adapters/              driven adapters (persistence, FCM)
package notifications

import (
	"context"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
)

type (
	DeviceStore = application.DeviceRepository
	Pusher      = application.Pusher
	TodoCreated = domain.TodoCreated
	// DueAlert is a todo's deadline reminder (due soon or overdue).
	DueAlert     = domain.DueAlert
	DueAlertKind = domain.DueAlertKind
)

const (
	DueSoon = domain.DueSoon
	Overdue = domain.Overdue
)

// Job kinds the module runs in the worker.
const (
	jobTodoCreated = "notifications.todo_created"
	jobPush        = "notifications.push"
	// sendTimeout bounds one notification, from the roster lookup to FCM.
	sendTimeout = 30 * time.Second
)

type Deps struct {
	Devices   DeviceStore
	Directory access.Directory
	Pusher    Pusher // nil: devices are kept, nothing is sent
	// Jobs records notifications to send; the worker sends them.
	Jobs jobs.Enqueuer
	Now  func() time.Time // nil means time.Now
}

type Module struct {
	Commands command.Handlers
	Queries  query.Handlers
	jobs     jobs.Enqueuer
}

func New(d Deps) *Module {
	return &Module{
		Commands: command.New(command.Deps{
			Devices:   d.Devices,
			Directory: d.Directory,
			Pusher:    d.Pusher,
			Now:       d.Now,
		}),
		Queries: query.New(query.Deps{Devices: d.Devices}),
		jobs:    d.Jobs,
	}
}

// RegisterRoutes mounts /me/devices; protect must authenticate the caller
// and resolve their memberships (access.Middleware).
func (m *Module) RegisterRoutes(mux *http.ServeMux, protect httpx.Middleware) {
	httpapi.Register(mux, m.Commands, m.Queries, protect)
}

// TodoCreated records that the team should hear about a new task, if the
// event calls for it. Called inside the caller's unit of work, the job
// commits with the task; the worker sends it, so a push outage never fails
// or slows adding a task, and a crash can't lose it.
func (m *Module) TodoCreated(ctx context.Context, e TodoCreated) error {
	if !e.Notifies() {
		return nil
	}
	_, err := m.jobs.Enqueue(ctx, jobTodoCreated, e)
	return err
}

// Push is a notification for some users, built by another module (e.g. a
// due-date alert).
type Push struct {
	UserIDs []string       `json:"userIds"`
	Message domain.Message `json:"message"`
}

// PushToUsers records a notification for users; the worker sends it to
// their devices. key, when not "", sends it at most once (see
// jobs.UniqueKey).
func (m *Module) PushToUsers(ctx context.Context, p Push, key string) error {
	opts := []jobs.Option{}
	if key != "" {
		opts = append(opts, jobs.UniqueKey(key))
	}
	_, err := m.jobs.Enqueue(ctx, jobPush, p, opts...)
	return err
}

// TodoDue records a due-date alert for the todo's user, worded as of now;
// the worker sends it. Called inside the caller's unit of work, it commits
// with the caller's change (marking the todo alerted). Each alert is
// enqueued at most once per todo, kind and deadline (DueAlert.Key), so a
// rerun of the caller's job sends nothing twice.
func (m *Module) TodoDue(ctx context.Context, e DueAlert, now time.Time) error {
	return m.PushToUsers(ctx, Push{UserIDs: []string{e.UserID}, Message: e.Message(now)}, e.Key())
}

// RegisterJobs adds the module's job handlers. A failed delivery to FCM is
// not retried (the devices that got it would get it twice); a failure
// before sending (the database) is.
func (m *Module) RegisterJobs(r *jobs.Registry) {
	jobs.Handle(r, jobTodoCreated, sendTimeout, func(ctx context.Context, e TodoCreated) error {
		return m.Commands.NotifyTodoCreated.Handle(ctx, command.NotifyTodoCreated{Event: e})
	})
	jobs.Handle(r, jobPush, sendTimeout, func(ctx context.Context, p Push) error {
		return m.Commands.NotifyUsers.Handle(ctx, command.NotifyUsers{UserIDs: p.UserIDs, Message: p.Message})
	})
}

// DeleteUserDevices forgets a deleted account's devices.
func (m *Module) DeleteUserDevices(ctx context.Context, userID string) error {
	return m.Commands.DeleteUserDevices.Handle(ctx, command.DeleteUserDevices{UserID: userID})
}
