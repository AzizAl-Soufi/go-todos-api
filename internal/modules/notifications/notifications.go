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
	"log/slog"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

type (
	DeviceStore = application.DeviceRepository
	Pusher      = application.Pusher
	TodoCreated = domain.TodoCreated
)

// sendTimeout bounds one notification, from the roster lookup to FCM.
const sendTimeout = 30 * time.Second

type Deps struct {
	Devices   DeviceStore
	Directory access.Directory
	Pusher    Pusher // nil: devices are kept, nothing is sent
	// Dispatch runs a notification after the request that caused it; nil
	// means in the background, at most maxInFlight at a time, drained by
	// Close. Tests pass one that runs it in place.
	Dispatch func(func())
	Now      func() time.Time // nil means time.Now
}

type Module struct {
	Commands   command.Handlers
	Queries    query.Handlers
	dispatch   func(func())
	background *background // nil when Deps.Dispatch was given
}

func New(d Deps) *Module {
	var bg *background
	if d.Dispatch == nil {
		bg = newBackground(maxInFlight)
		d.Dispatch = bg.run
	}
	return &Module{
		background: bg,
		Commands: command.New(command.Deps{
			Devices:   d.Devices,
			Directory: d.Directory,
			Pusher:    d.Pusher,
			Now:       d.Now,
		}),
		Queries:  query.New(query.Deps{Devices: d.Devices}),
		dispatch: d.Dispatch,
	}
}

// RegisterRoutes mounts /me/devices; protect must authenticate the caller
// and resolve their memberships (access.Middleware).
func (m *Module) RegisterRoutes(mux *http.ServeMux, protect httpx.Middleware) {
	httpapi.Register(mux, m.Commands, m.Queries, protect)
}

// TodoCreated notifies the team about a new task, if the event calls for
// it. It returns at once: the notification is sent in the background, after
// the caller's transaction, and failures are only logged, so a push outage
// never fails or slows adding a task.
func (m *Module) TodoCreated(ctx context.Context, e TodoCreated) {
	if !e.Notifies() {
		return
	}
	ctx = context.WithoutCancel(ctx)
	m.dispatch(func() {
		ctx, cancel := context.WithTimeout(ctx, sendTimeout)
		defer cancel()
		err := m.Commands.NotifyTodoCreated.Handle(ctx, command.NotifyTodoCreated{Event: e})
		if err != nil {
			slog.Error("failed to send task notification",
				"team_id", e.TeamID,
				"todo_id", e.TodoID,
				"error", err,
			)
		}
	})
}

// Close stops sending new notifications and waits for those in flight,
// until ctx ends (then it returns ctx's error). Call it after the HTTP server
// has shut down and before the database is closed.
func (m *Module) Close(ctx context.Context) error {
	if m.background == nil {
		return nil
	}
	return m.background.close(ctx)
}

// DeleteUserDevices forgets a deleted account's devices.
func (m *Module) DeleteUserDevices(ctx context.Context, userID string) error {
	return m.Commands.DeleteUserDevices.Handle(ctx, command.DeleteUserDevices{UserID: userID})
}
