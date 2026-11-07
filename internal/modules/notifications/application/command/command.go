// Package command holds the notifications module's write use cases, one
// handler per command.
package command

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

type Deps struct {
	Devices   application.DeviceRepository
	Directory application.Directory
	// Pusher is nil when push notifications are off (no Firebase service
	// account): devices are still registered, nothing is sent.
	Pusher application.Pusher
	Now    func() time.Time // nil means time.Now
}

type Handlers struct {
	RegisterDevice    RegisterDeviceHandler
	UnregisterDevice  UnregisterDeviceHandler
	DeleteUserDevices DeleteUserDevicesHandler
	NotifyTodoCreated NotifyTodoCreatedHandler
}

func New(d Deps) Handlers {
	if d.Now == nil {
		d.Now = time.Now
	}
	return Handlers{
		RegisterDevice:    RegisterDeviceHandler{d},
		UnregisterDevice:  UnregisterDeviceHandler{d},
		DeleteUserDevices: DeleteUserDevicesHandler{d},
		NotifyTodoCreated: NotifyTodoCreatedHandler{d},
	}
}

// RegisterDevice stores (or refreshes) the caller's FCM registration token.
// Clients call it after sign-in and whenever FCM hands them a new token. A
// user keeps at most domain.MaxDevicesPerUser devices; the least recently
// registered ones beyond that are dropped.
type RegisterDevice struct {
	UserID   string
	Token    string
	Platform string
}

type RegisterDeviceHandler struct{ Deps }

// Handle returns the normalized token.
func (h RegisterDeviceHandler) Handle(ctx context.Context, cmd RegisterDevice) (string, error) {
	d, err := domain.NewDevice(cmd.UserID, cmd.Token, cmd.Platform, h.Now())
	if err != nil {
		return "", err
	}
	if err := h.Devices.Save(ctx, d); err != nil {
		return "", err
	}
	devices, err := h.Devices.ListForUser(ctx, cmd.UserID)
	if err != nil {
		return "", err
	}
	excess := max(0, len(devices)-domain.MaxDevicesPerUser)
	for _, old := range devices[:excess] {
		err := h.Devices.Delete(ctx, cmd.UserID, old.Token)
		if err != nil && !errors.Is(err, apperrors.ErrNotFound) { // already gone is fine
			return "", err
		}
	}
	return d.Token, nil
}

// UnregisterDevice removes one of the caller's devices (e.g. on sign-out),
// named by its ID (domain.DeviceID), never by its token.
type UnregisterDevice struct {
	UserID   string
	DeviceID string
}

type UnregisterDeviceHandler struct{ Deps }

func (h UnregisterDeviceHandler) Handle(ctx context.Context, cmd UnregisterDevice) error {
	devices, err := h.Devices.ListForUser(ctx, cmd.UserID)
	if err != nil {
		return err
	}
	// A user has at most domain.MaxDevicesPerUser devices, so matching the
	// ID here needs no lookup by hash in the stores.
	i := slices.IndexFunc(devices, func(d *domain.Device) bool { return d.ID() == cmd.DeviceID })
	if i < 0 {
		return domain.ErrDeviceNotFound
	}
	return apperrors.NotFoundAs(h.Devices.Delete(ctx, cmd.UserID, devices[i].Token), domain.ErrDeviceNotFound)
}

// DeleteUserDevices forgets every device of a deleted account.
type DeleteUserDevices struct {
	UserID string
}

type DeleteUserDevicesHandler struct{ Deps }

func (h DeleteUserDevicesHandler) Handle(ctx context.Context, cmd DeleteUserDevices) error {
	return h.Devices.DeleteForUser(ctx, cmd.UserID)
}

// NotifyTodoCreated pushes "new task" to the team's other members when its
// owner or an admin adds a task (domain.TodoCreated.Notifies). It runs after
// the todo was committed; delivery is best effort.
type NotifyTodoCreated struct {
	Event domain.TodoCreated
}

type NotifyTodoCreatedHandler struct{ Deps }

func (h NotifyTodoCreatedHandler) Handle(ctx context.Context, cmd NotifyTodoCreated) error {
	e := cmd.Event
	if h.Pusher == nil || !e.Notifies() {
		return nil
	}
	roster, err := h.Directory.Roster(ctx, e.TeamID)
	if errors.Is(err, access.ErrTeamNotFound) {
		return nil // deleted in the meantime
	}
	if err != nil {
		return err
	}
	recipients := e.Recipients(roster)
	if len(recipients) == 0 {
		return nil
	}
	devices, err := h.Devices.ListForUsers(ctx, recipients)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return nil
	}
	tokens := make([]string, len(devices))
	for i, d := range devices {
		tokens[i] = d.Token
	}
	invalid, pushErr := h.Pusher.Push(ctx, tokens, e.Message(roster.Name))
	if len(invalid) > 0 {
		if err := h.Devices.DeleteTokens(ctx, invalid); err != nil {
			slog.Error("failed to forget invalid device tokens", "count", len(invalid), "error", err)
		}
	}
	return pushErr
}
