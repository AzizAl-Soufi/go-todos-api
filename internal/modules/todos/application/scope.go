package application

import (
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
)

// ScopeOf is what an actor can reach: their personal records and those of
// every team they belong to.
func ScopeOf(a access.Actor) domain.Scope {
	return domain.Scope{UserID: a.UserID, TeamIDs: a.TeamIDs()}
}
