package access

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
)

func TestRoles(t *testing.T) {
	for _, tc := range []struct {
		role           Role
		edit, manage   bool
		outranksMember bool
	}{
		{Owner, true, true, true},
		{Admin, true, true, true},
		{Member, true, false, false},
		{Viewer, false, false, false},
		{Role("bogus"), false, false, false},
	} {
		if tc.role.CanEdit() != tc.edit || tc.role.CanManage() != tc.manage || tc.role.Outranks(Member) != tc.outranksMember {
			t.Errorf("%s: edit=%v manage=%v outranks member=%v", tc.role, tc.role.CanEdit(), tc.role.CanManage(), tc.role.Outranks(Member))
		}
	}
	if Admin.Outranks(Admin) || !Owner.Outranks(Admin) || Viewer.Outranks(Role("bogus")) == false {
		t.Fatal("Outranks must be strict and rank unknown roles lowest")
	}
	if _, err := ParseRole("admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRole("Admin"); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("roles are case-sensitive: %v", err)
	}
}

func TestActor(t *testing.T) {
	a := Actor{UserID: "u", Teams: map[string]Role{"t2": Viewer, "t1": Admin}}
	if got := a.TeamIDs(); !slices.Equal(got, []string{"t1", "t2"}) {
		t.Fatalf("TeamIDs = %v", got)
	}
	if err := a.Require("t1", Role.CanManage); err != nil {
		t.Fatal(err)
	}
	if err := a.Require("t2", Role.CanEdit); !errors.Is(err, ErrInsufficientRole) {
		t.Fatalf("viewer editing: %v", err)
	}
	if err := a.Require("t3", Role.CanEdit); !errors.Is(err, ErrTeamNotFound) {
		t.Fatalf("non-member: %v", err)
	}
	if len(Personal("u").TeamIDs()) != 0 {
		t.Fatal("a personal actor has no teams")
	}
}

type resolver map[string]Role

func (r resolver) Memberships(context.Context, string) (map[string]Role, error) { return r, nil }

func TestMiddleware(t *testing.T) {
	var seen Actor
	h := Middleware(resolver{"t": Member})(ForActor(func(r *http.Request, a Actor) (int, any, error) {
		seen = a
		return http.StatusNoContent, nil, nil
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("without principal: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{UserID: "u", Email: "u@x.co"})
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	if rec.Code != http.StatusNoContent || seen.UserID != "u" || seen.Email != "u@x.co" || seen.Teams["t"] != Member {
		t.Fatalf("status %d, actor %+v", rec.Code, seen)
	}

	rec = httptest.NewRecorder()
	ForActor(func(*http.Request, Actor) (int, any, error) { return 200, nil, nil }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ForActor without actor: %d", rec.Code)
	}
}
