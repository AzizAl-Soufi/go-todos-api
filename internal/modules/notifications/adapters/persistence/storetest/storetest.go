// Package storetest is the contract every device store must satisfy; the
// memory, MongoDB and PostgreSQL stores all run it.
package storetest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// Stores is one backend's device store on empty storage, plus well-formed
// user IDs for that backend.
type Stores struct {
	Devices application.DeviceRepository
	NewID   func() string
}

func Run(t *testing.T, newStores func(t *testing.T) Stores) {
	t.Run("Devices", func(t *testing.T) { testDevices(t, newStores(t)) })
	t.Run("Reregistering", func(t *testing.T) { testReregistering(t, newStores(t)) })
	t.Run("Deleting", func(t *testing.T) { testDeleting(t, newStores(t)) })
}

var ctx = context.Background()

// now is truncated to what every backend stores (MongoDB keeps milliseconds).
func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func save(t *testing.T, s Stores, userID, token string, at time.Time) {
	t.Helper()
	d := &domain.Device{Token: token, UserID: userID, Platform: domain.Android, CreatedAt: at, UpdatedAt: at}
	if err := s.Devices.Save(ctx, d); err != nil {
		t.Fatalf("save %s: %v", token, err)
	}
}

// tokensOf returns a function that takes a list call's results and returns
// the tokens, failing the test on an error.
func tokensOf(t *testing.T) func([]*domain.Device, error) []string {
	return func(list []*domain.Device, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, d := range list {
			out = append(out, d.Token)
		}
		return out
	}
}

func testDevices(t *testing.T, s Stores) {
	ann, bo, cy := s.NewID(), s.NewID(), s.NewID()
	at := now()
	save(t, s, ann, "ann-phone", at.Add(time.Minute))
	save(t, s, ann, "ann-web", at)
	save(t, s, bo, "bo-phone", at)

	list, err := s.Devices.ListForUser(ctx, ann)
	if got := tokensOf(t)(list, err); !slices.Equal(got, []string{"ann-web", "ann-phone"}) {
		t.Fatalf("ListForUser = %v, want least recently registered first", got)
	}
	d := list[0]
	sameTimes := d.CreatedAt.Equal(at) && d.UpdatedAt.Equal(at)
	if d.UserID != ann || d.Platform != domain.Android || !sameTimes {
		t.Fatalf("round trip: %+v", d)
	}
	if got := tokensOf(t)(s.Devices.ListForUsers(ctx, []string{ann, bo})); len(got) != 3 {
		t.Fatalf("ListForUsers(ann, bo) = %v", got)
	}
	if got := tokensOf(t)(s.Devices.ListForUsers(ctx, []string{bo, cy})); !slices.Equal(got, []string{"bo-phone"}) {
		t.Fatalf("ListForUsers(bo, cy) = %v", got)
	}
	if got := tokensOf(t)(s.Devices.ListForUsers(ctx, nil)); len(got) != 0 {
		t.Fatalf("ListForUsers(none) = %v", got)
	}
	if got := tokensOf(t)(s.Devices.ListForUser(ctx, cy)); got == nil || len(got) != 0 {
		t.Fatalf("no devices must be an empty list: %#v", got)
	}
}

// A token registered again is refreshed, and moves to whoever registered it.
func testReregistering(t *testing.T, s Stores) {
	ann, bo := s.NewID(), s.NewID()
	first := now()
	save(t, s, ann, "shared", first)
	later := first.Add(time.Hour)
	d := &domain.Device{Token: "shared", UserID: bo, Platform: domain.Web, CreatedAt: later, UpdatedAt: later}
	if err := s.Devices.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	if got := tokensOf(t)(s.Devices.ListForUser(ctx, ann)); len(got) != 0 {
		t.Fatalf("token still with its previous user: %v", got)
	}
	list, err := s.Devices.ListForUser(ctx, bo)
	if err != nil || len(list) != 1 {
		t.Fatalf("moved device: %v, %v", list, err)
	}
	if got := list[0]; got.Platform != domain.Web || !got.CreatedAt.Equal(first) || !got.UpdatedAt.Equal(later) {
		t.Fatalf("re-registered device = %+v, want platform updated, created at kept", got)
	}
}

func testDeleting(t *testing.T, s Stores) {
	ann, bo := s.NewID(), s.NewID()
	at := now()
	for _, tok := range []string{"a1", "a2", "a3"} {
		save(t, s, ann, tok, at)
	}
	save(t, s, bo, "b1", at)
	save(t, s, bo, "b2", at)

	if err := s.Devices.Delete(ctx, bo, "a1"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleting another user's device: err = %v, want ErrNotFound", err)
	}
	if err := s.Devices.Delete(ctx, ann, "missing"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleting a missing device: err = %v, want ErrNotFound", err)
	}
	if err := s.Devices.Delete(ctx, ann, "a1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Devices.DeleteTokens(ctx, []string{"a2", "b1", "unknown"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Devices.DeleteTokens(ctx, nil); err != nil {
		t.Fatalf("deleting no tokens: %v", err)
	}
	got := slices.Sorted(slices.Values(tokensOf(t)(s.Devices.ListForUsers(ctx, []string{ann, bo}))))
	if !slices.Equal(got, []string{"a3", "b2"}) {
		t.Fatalf("after deletes: %v", got)
	}
	if err := s.Devices.DeleteForUser(ctx, ann); err != nil {
		t.Fatal(err)
	}
	if got := tokensOf(t)(s.Devices.ListForUsers(ctx, []string{ann, bo})); !slices.Equal(got, []string{"b2"}) {
		t.Fatalf("DeleteForUser must delete exactly that user's devices: %v", got)
	}
}
