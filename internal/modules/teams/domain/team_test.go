package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

func TestTeamName(t *testing.T) {
	team, err := NewTeam("  Platform  ", time.Now())
	if err != nil || team.Name != "Platform" {
		t.Fatalf("team = %+v (%v)", team, err)
	}
	if _, err := NewTeam(" ", time.Now()); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("blank name: %v", err)
	}
	if err := team.Rename(strings.Repeat("x", MaxNameLength+1), time.Now()); !errors.Is(err, ErrNameTooLong) || team.Name != "Platform" {
		t.Fatalf("long name: %v, name %q", err, team.Name)
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got, err := NormalizeEmail(" Ann@Example.COM "); err != nil || got != "ann@example.com" {
		t.Fatalf("got %q (%v)", got, err)
	}
	for _, bad := range []string{"", "ann", "Ann <ann@example.com>"} {
		if _, err := NormalizeEmail(bad); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestPolicy(t *testing.T) {
	o, a, m, v := access.Owner, access.Admin, access.Member, access.Viewer
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"owner invites admin", CanInvite(o, a), nil},
		{"admin invites member", CanInvite(a, m), nil},
		{"admin invites admin", CanInvite(a, a), ErrInvitationRoleTooHigh},
		{"member invites viewer", CanInvite(m, v), access.ErrInsufficientRole},
		{"nobody invites an owner", CanInvite(o, o), ErrOwnerNotAssignable},

		{"owner promotes member to admin", CanChangeRole(o, m, a), nil},
		{"owner demotes admin", CanChangeRole(o, a, v), nil},
		{"admin promotes viewer to member", CanChangeRole(a, v, m), nil},
		{"admin promotes member to admin", CanChangeRole(a, m, a), access.ErrInsufficientRole},
		{"admin demotes admin", CanChangeRole(a, a, m), access.ErrInsufficientRole},
		{"admin demotes owner", CanChangeRole(a, o, m), access.ErrInsufficientRole},
		{"member changes viewer", CanChangeRole(m, v, m), access.ErrInsufficientRole},
		{"owner assigns owner", CanChangeRole(o, a, o), ErrOwnerNotAssignable},

		{"owner removes admin", CanRemove(o, a), nil},
		{"admin removes member", CanRemove(a, m), nil},
		{"admin removes admin", CanRemove(a, a), access.ErrInsufficientRole},
		{"member removes viewer", CanRemove(m, v), access.ErrInsufficientRole},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.want) && !(c.want == nil && c.err == nil) {
			t.Errorf("%s: err = %v, want %v", c.name, c.err, c.want)
		}
	}
}

func TestInvitationExpired(t *testing.T) {
	now := time.Now()
	inv := Invitation{ExpiresAt: now}
	if !inv.Expired(now) || inv.Expired(now.Add(-time.Second)) {
		t.Fatal("an invitation expires at ExpiresAt")
	}
}

func TestInvitationResend(t *testing.T) {
	sent := time.Date(2026, 11, 7, 12, 0, 0, 500_000_000, time.UTC)
	inv := &Invitation{SentAt: sent}
	if !inv.ResendAvailableAt().Equal(sent.Add(InvitationResendCooldown)) {
		t.Fatalf("ResendAvailableAt = %v", inv.ResendAvailableAt())
	}

	err := inv.CheckResend(sent.Add(time.Minute))
	var later apperrors.RetryLaterError
	if !errors.Is(err, ErrResendTooSoon) || !errors.As(err, &later) {
		t.Fatalf("a minute after sending: err = %v, want ErrResendTooSoon with a retry time", err)
	}
	// Rounded up to the whole second, so retrying at that time works.
	if want := time.Date(2026, 11, 7, 12, 2, 1, 0, time.UTC); !later.At.Equal(want) || later.Wait != 60500*time.Millisecond {
		t.Fatalf("retry at %v after %v, want %v after 60.5s", later.At, later.Wait, want)
	}

	if err := inv.CheckResend(sent.Add(InvitationResendCooldown)); err != nil {
		t.Fatalf("once the cooldown has passed: %v", err)
	}
}
