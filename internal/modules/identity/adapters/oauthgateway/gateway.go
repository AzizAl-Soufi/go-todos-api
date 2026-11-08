// Package oauthgateway connects the identity module to the oauth module:
// logging out everywhere and deleting the account disconnect every app.
package oauthgateway

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth"
)

type Gateway struct{ oauth *oauth.Module }

var _ application.Connections = Gateway{}

func New(m *oauth.Module) Gateway { return Gateway{oauth: m} }

func (g Gateway) RevokeUserConnections(ctx context.Context, userID string) error {
	return g.oauth.RevokeUserConnections(ctx, userID)
}
