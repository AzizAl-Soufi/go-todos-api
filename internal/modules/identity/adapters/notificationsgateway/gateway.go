// Package notificationsgateway connects the identity module to the
// notifications module: a deleted account's devices are forgotten.
package notificationsgateway

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications"
)

type Gateway struct{ notifications *notifications.Module }

var _ application.Devices = Gateway{}

func New(m *notifications.Module) Gateway { return Gateway{notifications: m} }

func (g Gateway) DeleteUserDevices(ctx context.Context, userID string) error {
	return g.notifications.DeleteUserDevices(ctx, userID)
}
