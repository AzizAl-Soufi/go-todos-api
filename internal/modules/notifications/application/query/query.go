// Package query holds the notifications module's read use cases.
package query

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
)

type Deps struct {
	Devices application.DeviceReader
}

type Handlers struct {
	ListDevices ListDevicesHandler
}

func New(d Deps) Handlers { return Handlers{ListDevices: ListDevicesHandler{d}} }

// ListDevices returns the caller's devices, least recently registered first.
type ListDevices struct {
	UserID string
}

type ListDevicesHandler struct{ d Deps }

func (h ListDevicesHandler) Handle(ctx context.Context, q ListDevices) ([]*domain.Device, error) {
	return h.d.Devices.ListForUser(ctx, q.UserID)
}
