package teams_test

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/opaque"
)

func TestNewDirectory(t *testing.T) {
	ctx := context.Background()
	teamStore, memberStore := memstore.NewTeams(), memstore.NewMembers()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	team, err := domain.NewTeam("Platform", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := teamStore.Create(ctx, team); err != nil {
		t.Fatal(err)
	}
	for user, role := range map[string]access.Role{"ann": access.Owner, "bo": access.Member} {
		m := &domain.Member{TeamID: team.ID, UserID: user, Role: role, JoinedAt: now}
		if err := memberStore.Add(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	dir := teams.NewDirectory(teamStore, memberStore)
	roster, err := dir.Roster(ctx, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantMembers := map[string]access.Role{"ann": access.Owner, "bo": access.Member}
	if roster.TeamID != team.ID || roster.Name != "Platform" || !maps.Equal(roster.Members, wantMembers) {
		t.Fatalf("roster = %+v", roster)
	}

	if _, err := dir.Roster(ctx, "000000000000000000000000"); !errors.Is(err, access.ErrTeamNotFound) {
		t.Fatalf("unknown team: err = %v, want ErrTeamNotFound", err)
	}
}

// mailbox keeps the links of the invitation emails sent.
type mailbox struct{ links []string }

func (m *mailbox) SendInvitation(_ context.Context, _, _, _ string, _ access.Role, link string) error {
	m.links = append(m.links, link)
	return nil
}

func (m *mailbox) lastToken(t *testing.T) string {
	t.Helper()
	if len(m.links) == 0 {
		t.Fatal("no invitation sent")
	}
	u, err := url.Parse(m.links[len(m.links)-1])
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("token")
}

type auditLog struct{ actions []string }

func (a *auditLog) Record(_ context.Context, e application.AuditEntry) error {
	a.actions = append(a.actions, e.Action)
	return nil
}

func (a *auditLog) DeleteTeamLog(context.Context, string) error { return nil }

type resendFixture struct {
	module      *teams.Module
	invitations *memstore.Invitations
	mail        *mailbox
	audit       *auditLog
	now         time.Time
	teamID      string
	owner       access.Actor
	admin       access.Actor
}

// newResendFixture has a team with an owner and an admin, at a fake time.
func newResendFixture(t *testing.T) *resendFixture {
	t.Helper()
	ctx := context.Background()
	f := &resendFixture{
		invitations: memstore.NewInvitations(),
		mail:        &mailbox{},
		audit:       &auditLog{},
		now:         time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC),
	}
	teamStore, memberStore := memstore.NewTeams(), memstore.NewMembers()
	team, err := domain.NewTeam("Platform", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := teamStore.Create(ctx, team); err != nil {
		t.Fatal(err)
	}
	f.teamID = team.ID
	for user, role := range map[string]access.Role{"ann": access.Owner, "bo": access.Admin} {
		if err := memberStore.Add(ctx, &domain.Member{TeamID: team.ID, UserID: user, Name: user, Role: role, JoinedAt: f.now}); err != nil {
			t.Fatal(err)
		}
	}
	f.owner = access.Actor{UserID: "ann", Teams: map[string]access.Role{team.ID: access.Owner}}
	f.admin = access.Actor{UserID: "bo", Teams: map[string]access.Role{team.ID: access.Admin}}
	f.module = teams.New(teams.Deps{
		Teams:       teamStore,
		Members:     memberStore,
		Invitations: f.invitations,
		Audit:       f.audit,
		Mailer:      f.mail,
		Config:      teams.DefaultConfig("http://example.test"),
		Now:         func() time.Time { return f.now },
	})
	return f
}

func (f *resendFixture) invite(t *testing.T, by access.Actor, email string, role access.Role) string {
	t.Helper()
	id, err := f.module.Commands.Invite.Handle(context.Background(), command.Invite{Actor: by, TeamID: f.teamID, Email: email, Role: role})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *resendFixture) resend(by access.Actor, id string) error {
	return f.module.Commands.ResendInvitation.Handle(context.Background(), command.ResendInvitation{Actor: by, TeamID: f.teamID, ID: id})
}

func TestResendInvitation(t *testing.T) {
	ctx := context.Background()
	f := newResendFixture(t)
	id := f.invite(t, f.owner, "cy@example.com", access.Member)
	firstToken := f.mail.lastToken(t)

	// Within 2 minutes of sending: refused, with when it becomes possible.
	f.now = f.now.Add(90 * time.Second)
	err := f.resend(f.owner, id)
	var later apperrors.RetryLaterError
	if !errors.Is(err, domain.ErrResendTooSoon) || !errors.As(err, &later) {
		t.Fatalf("resend after 90s: err = %v, want ErrResendTooSoon", err)
	}
	if want := time.Date(2026, 11, 7, 12, 2, 0, 0, time.UTC); !later.At.Equal(want) || later.Wait != 30*time.Second {
		t.Fatalf("retry at %v after %v, want %v after 30s", later.At, later.Wait, want)
	}
	if len(f.mail.links) != 1 {
		t.Fatalf("a refused resend sent mail: %v", f.mail.links)
	}

	// Once the cooldown has passed: a new link, a new expiry, same invitation.
	f.now = f.now.Add(30 * time.Second)
	if err := f.resend(f.owner, id); err != nil {
		t.Fatal(err)
	}
	newToken := f.mail.lastToken(t)
	if len(f.mail.links) != 2 || newToken == firstToken {
		t.Fatalf("links = %v, want a second, different link", f.mail.links)
	}
	if _, err := f.invitations.FindByTokenHash(ctx, opaque.Hash(firstToken)); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("old link still works: %v", err)
	}
	inv, err := f.invitations.FindByTokenHash(ctx, opaque.Hash(newToken))
	if err != nil || inv.ID != id || !inv.SentAt.Equal(f.now) || !inv.ExpiresAt.Equal(f.now.Add(7*24*time.Hour)) {
		t.Fatalf("resent invitation: %+v (%v)", inv, err)
	}
	if last := f.audit.actions[len(f.audit.actions)-1]; last != "invitation.resent" {
		t.Fatalf("audit = %v, want invitation.resent last", f.audit.actions)
	}

	// The cooldown starts again from the resend.
	f.now = f.now.Add(time.Minute)
	if err := f.resend(f.owner, id); !errors.Is(err, domain.ErrResendTooSoon) {
		t.Fatalf("resend a minute after a resend: err = %v", err)
	}
}

func TestInvite_AgainWaitsForTheCooldown(t *testing.T) {
	f := newResendFixture(t)
	first := f.invite(t, f.owner, "cy@example.com", access.Viewer)
	other := f.invite(t, f.owner, "di@example.com", access.Viewer) // other addresses aren't held up

	f.now = f.now.Add(time.Minute)
	_, err := f.module.Commands.Invite.Handle(context.Background(),
		command.Invite{Actor: f.owner, TeamID: f.teamID, Email: "CY@example.com", Role: access.Member})
	var later apperrors.RetryLaterError
	if !errors.Is(err, domain.ErrResendTooSoon) || !errors.As(err, &later) {
		t.Fatalf("re-invite after a minute: err = %v, want ErrResendTooSoon", err)
	}
	if want := time.Date(2026, 11, 7, 12, 2, 0, 0, time.UTC); !later.At.Equal(want) {
		t.Fatalf("retry at %v, want %v", later.At, want)
	}
	if len(f.mail.links) != 2 {
		t.Fatalf("a refused re-invite sent mail: %v", f.mail.links)
	}

	f.now = f.now.Add(time.Minute)
	again := f.invite(t, f.owner, "cy@example.com", access.Member)
	if again == first {
		t.Fatal("re-inviting replaces the invitation (new ID)")
	}
	list, err := f.invitations.ListByTeam(context.Background(), f.teamID)
	if err != nil || len(list) != 2 || list[0].ID != other || list[1].ID != again || list[1].Role != access.Member {
		t.Fatalf("pending after re-invite: %+v (%v)", list, err)
	}
}

func TestResendInvitation_ExpiredInvitationIsRenewed(t *testing.T) {
	f := newResendFixture(t)
	id := f.invite(t, f.owner, "cy@example.com", access.Viewer)
	f.now = f.now.Add(8 * 24 * time.Hour) // past the 7-day expiry
	if err := f.resend(f.owner, id); err != nil {
		t.Fatal(err)
	}
	inv, err := f.invitations.Get(context.Background(), f.teamID, id)
	if err != nil || inv.Expired(f.now) {
		t.Fatalf("invitation still expired after a resend: %+v (%v)", inv, err)
	}
}

func TestResendInvitation_Rules(t *testing.T) {
	f := newResendFixture(t)
	adminInvite := f.invite(t, f.owner, "di@example.com", access.Admin)
	memberInvite := f.invite(t, f.owner, "cy@example.com", access.Member)
	f.now = f.now.Add(time.Hour)

	outsider := access.Actor{UserID: "zed"}
	for name, c := range map[string]struct {
		by   access.Actor
		id   string
		want error
	}{
		"admin resends an admin invitation": {f.admin, adminInvite, domain.ErrInvitationRoleTooHigh},
		"non-member":                        {outsider, memberInvite, access.ErrTeamNotFound},
		"unknown invitation":                {f.owner, "000000000000000000000000", domain.ErrInvitationNotFound},
		"malformed invitation ID":           {f.owner, "nope", domain.ErrInvitationNotFound},
	} {
		if err := f.resend(c.by, c.id); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	if err := f.resend(f.admin, memberInvite); err != nil {
		t.Fatalf("admin resends a member invitation: %v", err)
	}
}
