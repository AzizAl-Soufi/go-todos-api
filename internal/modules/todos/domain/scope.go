package domain

import (
	"slices"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

var (
	// ErrTeamMismatch: a project and its collection, or a todo and its
	// project, always belong to the same team (or are both personal).
	ErrTeamMismatch = apperrors.Validation("TEAM_MISMATCH", "a project and its collection must belong to the same team, or both be personal")
	ErrTeamFixed    = apperrors.Validation("TEAM_IMMUTABLE", "the team of a project or collection is set when it is created and can't be changed")
)

// Scope is what a caller can reach: their own personal records and the
// records of the teams listed. Every repository call is scoped, so a record
// outside the scope looks exactly like a missing one.
type Scope struct {
	UserID  string
	TeamIDs []string
}

// Includes reports whether a record owned by (userID, teamID) is in scope.
// teamID "" is a personal record, reachable only by its user.
func (s Scope) Includes(userID, teamID string) bool {
	if teamID == "" {
		return userID == s.UserID
	}
	return slices.Contains(s.TeamIDs, teamID)
}

// OwnerFilter narrows a listing to personal records or to one team's. The
// zero value keeps everything in scope.
type OwnerFilter struct {
	TeamID   string
	Personal bool
}

func (f OwnerFilter) Matches(teamID string) bool {
	switch {
	case f.Personal:
		return teamID == ""
	case f.TeamID != "":
		return teamID == f.TeamID
	}
	return true
}
