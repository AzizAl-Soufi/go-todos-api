// Package storetest is the contract every teams persistence adapter must
// satisfy; the memory, MongoDB and PostgreSQL stores all run it.
package storetest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

type Backend struct {
	Teams       application.TeamRepository
	Members     application.MemberRepository
	Invitations application.InvitationRepository
	Tx          tx.Runner
	// AtomicTx is true when Tx really rolls back (not tx.None).
	AtomicTx bool
	// NewID returns a fresh, well-formed ID (users).
	NewID func() string
	// MissingID is a well-formed ID that matches nothing.
	MissingID string
}

func Run(t *testing.T, newBackend func(t *testing.T) Backend) {
	t.Run("Teams", func(t *testing.T) { testTeams(t, newBackend(t)) })
	t.Run("Members", func(t *testing.T) { testMembers(t, newBackend(t)) })
	t.Run("Invitations", func(t *testing.T) { testInvitations(t, newBackend(t)) })
	t.Run("Transactions", func(t *testing.T) {
		b := newBackend(t)
		if !b.AtomicTx {
			t.Skip("this backend runs without transactions")
		}
		testTransactions(t, b)
	})
}

var (
	ctx  = context.Background()
	base = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
)

func newTeam(t *testing.T, b Backend, name string) *domain.Team {
	t.Helper()
	team := &domain.Team{Name: name, CreatedAt: base, UpdatedAt: base}
	if err := b.Teams.Create(ctx, team); err != nil || team.ID == "" {
		t.Fatalf("create team: %v, id %q", err, team.ID)
	}
	return team
}

func addMember(t *testing.T, b Backend, teamID, userID string, role access.Role, joined time.Duration) *domain.Member {
	t.Helper()
	m := &domain.Member{TeamID: teamID, UserID: userID, Name: "N " + userID[:4], Email: userID + "@example.com", Role: role, JoinedAt: base.Add(joined)}
	if err := b.Members.Add(ctx, m); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return m
}

func expectNotFound(t *testing.T, cases map[string]error) {
	t.Helper()
	for name, err := range cases {
		if !errors.Is(err, apperrors.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
}

func testTeams(t *testing.T, b Backend) {
	team := newTeam(t, b, "Platform")
	got, err := b.Teams.Get(ctx, team.ID)
	if err != nil || got.Name != "Platform" || !got.CreatedAt.Equal(base) || !got.UpdatedAt.Equal(base) {
		t.Fatalf("round trip: %+v (%v)", got, err)
	}
	team.Name, team.UpdatedAt = "Core", base.Add(time.Hour)
	if err := b.Teams.Update(ctx, team); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Teams.Get(ctx, team.ID); got.Name != "Core" || !got.UpdatedAt.Equal(team.UpdatedAt) {
		t.Fatalf("update not saved: %+v", got)
	}
	expectNotFound(t, map[string]error{
		"Get missing":    func() error { _, err := b.Teams.Get(ctx, b.MissingID); return err }(),
		"Update missing": b.Teams.Update(ctx, &domain.Team{ID: b.MissingID, Name: "x"}),
		"Delete missing": b.Teams.Delete(ctx, b.MissingID),
	})
	if err := b.Teams.Delete(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Teams.Get(ctx, team.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleted team: %v", err)
	}
}

func userIDs(list []*domain.Member) []string {
	out := []string{}
	for _, m := range list {
		out = append(out, m.UserID)
	}
	return out
}

func testMembers(t *testing.T, b Backend) {
	team, other := newTeam(t, b, "A"), newTeam(t, b, "B")
	ann, bo, cy := b.NewID(), b.NewID(), b.NewID()
	owner := addMember(t, b, team.ID, ann, access.Owner, 0)
	addMember(t, b, team.ID, cy, access.Viewer, 2*time.Second)
	addMember(t, b, team.ID, bo, access.Member, time.Second)
	addMember(t, b, other.ID, ann, access.Viewer, -time.Second)

	got, err := b.Members.Get(ctx, team.ID, ann)
	if err != nil || got.TeamID != owner.TeamID || got.UserID != owner.UserID || got.Name != owner.Name ||
		got.Email != owner.Email || got.Role != owner.Role || !got.JoinedAt.Equal(owner.JoinedAt) {
		t.Fatalf("round trip:\n got  %+v (%v)\n want %+v", got, err, owner)
	}
	if err := b.Members.Add(ctx, &domain.Member{TeamID: team.ID, UserID: ann, Name: "x", Email: "x@x.co", Role: access.Member, JoinedAt: base}); !errors.Is(err, apperrors.ErrDuplicate) {
		t.Fatalf("duplicate membership: err = %v, want ErrDuplicate", err)
	}

	list, err := b.Members.ListByTeam(ctx, team.ID)
	if err != nil || !slices.Equal(userIDs(list), []string{ann, bo, cy}) {
		t.Fatalf("ListByTeam = %v (%v), want earliest joined first", userIDs(list), err)
	}
	mine, err := b.Members.ListByUser(ctx, ann)
	if err != nil || len(mine) != 2 || mine[0].TeamID != other.ID || mine[1].TeamID != team.ID {
		t.Fatalf("ListByUser = %+v (%v)", mine, err)
	}

	if err := b.Members.SetRole(ctx, team.ID, cy, access.Admin); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Members.Get(ctx, team.ID, cy); got.Role != access.Admin {
		t.Fatalf("SetRole not saved: %+v", got)
	}
	expectNotFound(t, map[string]error{
		"Get non-member":     func() error { _, err := b.Members.Get(ctx, other.ID, bo); return err }(),
		"SetRole non-member": b.Members.SetRole(ctx, other.ID, bo, access.Admin),
		"Remove non-member":  b.Members.Remove(ctx, other.ID, bo),
	})

	if err := b.Members.Remove(ctx, team.ID, bo); err != nil {
		t.Fatal(err)
	}
	if list, _ := b.Members.ListByTeam(ctx, team.ID); !slices.Equal(userIDs(list), []string{ann, cy}) {
		t.Fatalf("after Remove: %v", userIDs(list))
	}
	if err := b.Members.DeleteByTeam(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := b.Members.ListByTeam(ctx, team.ID); len(list) != 0 {
		t.Fatal("DeleteByTeam left members")
	}
	if mine, _ := b.Members.ListByUser(ctx, ann); len(mine) != 1 {
		t.Fatal("DeleteByTeam removed another team's membership")
	}
}

func newInvitation(t *testing.T, b Backend, teamID, email, hash string, created time.Duration) *domain.Invitation {
	t.Helper()
	inv := &domain.Invitation{
		TeamID: teamID, Email: email, Role: access.Member, InvitedBy: b.NewID(), TokenHash: hash,
		ExpiresAt: base.Add(created + 24*time.Hour), CreatedAt: base.Add(created), SentAt: base.Add(created),
	}
	if err := b.Invitations.Save(ctx, inv); err != nil || inv.ID == "" {
		t.Fatalf("save invitation: %v, id %q", err, inv.ID)
	}
	return inv
}

func testInvitations(t *testing.T, b Backend) {
	team, other := newTeam(t, b, "A"), newTeam(t, b, "B")
	first := newInvitation(t, b, team.ID, "bo@example.com", "hash-1", 0)
	newInvitation(t, b, team.ID, "cy@example.com", "hash-2", time.Second)
	theirs := newInvitation(t, b, other.ID, "bo@example.com", "hash-3", 0)

	got, err := b.Invitations.FindByTokenHash(ctx, "hash-1")
	if err != nil || got.ID != first.ID || got.TeamID != team.ID || got.Email != "bo@example.com" || got.Role != access.Member ||
		got.InvitedBy != first.InvitedBy || !got.ExpiresAt.Equal(first.ExpiresAt) || !got.CreatedAt.Equal(first.CreatedAt) ||
		!got.SentAt.Equal(first.SentAt) {
		t.Fatalf("round trip:\n got  %+v (%v)\n want %+v", got, err, first)
	}

	// Inviting the same email again replaces the pending invitation.
	again := newInvitation(t, b, team.ID, "bo@example.com", "hash-4", 2*time.Second)
	if _, err := b.Invitations.FindByTokenHash(ctx, "hash-1"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("replaced invitation's token still works: %v", err)
	}
	list, err := b.Invitations.ListByTeam(ctx, team.ID)
	if err != nil || len(list) != 2 || list[0].Email != "cy@example.com" || list[1].ID != again.ID {
		t.Fatalf("ListByTeam = %+v (%v), want oldest first, one per email", list, err)
	}
	if got, err := b.Invitations.Get(ctx, team.ID, again.ID); err != nil || got.TokenHash != "hash-4" {
		t.Fatalf("Get: %+v (%v)", got, err)
	}

	testResend(t, b, team.ID, again)

	expectNotFound(t, map[string]error{
		"Resend in another team": b.Invitations.Resend(ctx, application.InvitationResend{
			TeamID: team.ID, ID: theirs.ID, TokenHash: "hash-x", SentBefore: base.Add(time.Hour),
		}),
		"Get from another team":    func() error { _, err := b.Invitations.Get(ctx, team.ID, theirs.ID); return err }(),
		"Delete from another team": b.Invitations.Delete(ctx, team.ID, theirs.ID),
		"Find unknown token":       func() error { _, err := b.Invitations.FindByTokenHash(ctx, "nope"); return err }(),
		"Delete missing":           b.Invitations.Delete(ctx, team.ID, b.MissingID),
	})

	testDeleteExpired(t, b, other.ID)

	if err := b.Invitations.Delete(ctx, team.ID, again.ID); err != nil {
		t.Fatal(err)
	}
	if err := b.Invitations.DeleteByTeam(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := b.Invitations.ListByTeam(ctx, team.ID); len(list) != 0 {
		t.Fatal("DeleteByTeam left invitations")
	}
	if _, err := b.Invitations.FindByTokenHash(ctx, "hash-3"); err != nil {
		t.Fatal("DeleteByTeam removed another team's invitation")
	}
}

// testDeleteExpired checks the periodic cleanup: invitations that expired
// before the cutoff go, in batches; newer ones stay.
func testDeleteExpired(t *testing.T, b Backend, teamID string) {
	t.Helper()
	for i, email := range []string{"old1@example.com", "old2@example.com", "old3@example.com"} {
		inv := newInvitation(t, b, teamID, email, "expired-"+email, time.Duration(i)*time.Second)
		inv.ExpiresAt = base.Add(-48 * time.Hour)
		if err := b.Invitations.Save(ctx, inv); err != nil {
			t.Fatal(err)
		}
	}
	n, err := b.Invitations.DeleteExpired(ctx, base.Add(-time.Hour), 2)
	if err != nil || n != 2 {
		t.Fatalf("first batch: %d (%v), want 2", n, err)
	}
	if n, err = b.Invitations.DeleteExpired(ctx, base.Add(-time.Hour), 2); err != nil || n != 1 {
		t.Fatalf("second batch: %d (%v), want the last expired one", n, err)
	}
	if _, err := b.Invitations.FindByTokenHash(ctx, "hash-3"); err != nil {
		t.Fatalf("an unexpired invitation was deleted: %v", err)
	}
}

// testResend checks that a resend replaces the link, expiry and send time
// but keeps the invitation, and only happens once the cooldown has passed.
func testResend(t *testing.T, b Backend, teamID string, inv *domain.Invitation) {
	t.Helper()
	sentAt := inv.SentAt.Add(3 * time.Minute)
	resend := application.InvitationResend{
		TeamID:     teamID,
		ID:         inv.ID,
		TokenHash:  "hash-resent",
		SentAt:     sentAt,
		ExpiresAt:  sentAt.Add(24 * time.Hour),
		SentBefore: inv.SentAt.Add(-time.Second), // not yet: sent after this
	}
	if err := b.Invitations.Resend(ctx, resend); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("resend within the cooldown: err = %v, want ErrNotFound", err)
	}

	resend.SentBefore = inv.SentAt // sent exactly then: allowed
	if err := b.Invitations.Resend(ctx, resend); err != nil {
		t.Fatalf("resend after the cooldown: %v", err)
	}
	got, err := b.Invitations.Get(ctx, teamID, inv.ID)
	if err != nil || got.ID != inv.ID || got.TokenHash != "hash-resent" || !got.SentAt.Equal(sentAt) ||
		!got.ExpiresAt.Equal(resend.ExpiresAt) || !got.CreatedAt.Equal(inv.CreatedAt) || got.Email != inv.Email {
		t.Fatalf("after resend: %+v (%v)", got, err)
	}
	if _, err := b.Invitations.FindByTokenHash(ctx, inv.TokenHash); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("old link still works after a resend: %v", err)
	}

	// The same resend again: the send time it just set is after SentBefore.
	if err := b.Invitations.Resend(ctx, resend); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("second resend in the same cooldown: err = %v, want ErrNotFound", err)
	}
}

func testTransactions(t *testing.T, b Backend) {
	boom := errors.New("boom")
	var teamID string
	err := b.Tx.WithinTx(ctx, func(ctx context.Context) error {
		team := &domain.Team{Name: "Tx", CreatedAt: base, UpdatedAt: base}
		if err := b.Teams.Create(ctx, team); err != nil {
			return err
		}
		teamID = team.ID
		if err := b.Members.Add(ctx, &domain.Member{TeamID: team.ID, UserID: b.NewID(), Name: "N", Email: "n@x.co", Role: access.Owner, JoinedAt: base}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := b.Teams.Get(ctx, teamID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("team created in a failed transaction is visible: %v", err)
	}
	if list, _ := b.Members.ListByTeam(ctx, teamID); len(list) != 0 {
		t.Fatal("member added in a failed transaction is visible")
	}
}
