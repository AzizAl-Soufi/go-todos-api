// Package teamsgateway connects the identity module to the teams module's
// public API: a deleted account leaves its teams.
package teamsgateway

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams"
)

type Gateway struct{ teams *teams.Module }

var _ application.TeamMemberships = Gateway{}

func New(m *teams.Module) Gateway { return Gateway{teams: m} }

func (g Gateway) RemoveUser(ctx context.Context, userID string) error {
	return g.teams.RemoveUser(ctx, userID)
}
