package main

import (
	"context"
	"slices"
	"sync"
	"testing"

	notificationsdomain "github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
)

// fakePusher stands in for FCM: it records every push.
type fakePusher struct {
	mu     sync.Mutex
	pushes []sentPush
}

type sentPush struct {
	tokens []string
	msg    notificationsdomain.Message
}

func (p *fakePusher) Push(_ context.Context, tokens []string, m notificationsdomain.Message) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pushes = append(p.pushes, sentPush{slices.Sorted(slices.Values(tokens)), m})
	return nil, nil
}

func (p *fakePusher) sent() []sentPush {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.pushes)
}

func (s *testServer) device(sess session, token string) {
	s.t.Helper()
	s.do("POST", "/me/devices", sess.access, map[string]string{"token": token, "platform": "android"}).expect(s.t, 200)
}

func TestDevices(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	bo := s.register("Bo", "bo@example.com")

	s.do("GET", "/me/devices", "", nil).expectCode(t, 401, "MISSING_ACCESS_TOKEN")
	s.do("POST", "/me/devices", ann.access, map[string]string{"platform": "ios"}).expectCode(t, 400, "INVALID_DEVICE_TOKEN")
	s.do("POST", "/me/devices", ann.access, map[string]string{"token": "tok", "platform": "symbian"}).expectCode(t, 400, "INVALID_PLATFORM")

	r := s.do("POST", "/me/devices", ann.access, map[string]string{"token": "fcm:ann-phone", "platform": "iOS"}).expect(t, 200)
	if r.str("token") != "fcm:ann-phone" || r.str("platform") != "ios" || r.str("createdAt") == "" {
		t.Fatalf("registered device: %s", r.raw)
	}
	s.device(ann, "fcm:ann-phone") // registering again is fine
	if list := s.do("GET", "/me/devices", ann.access, nil).expect(t, 200).items(t); len(list) != 1 {
		t.Fatalf("devices = %v", list)
	}

	s.do("DELETE", "/me/devices/fcm:ann-phone", bo.access, nil).expectCode(t, 404, "DEVICE_NOT_FOUND")
	s.do("DELETE", "/me/devices/fcm:ann-phone", ann.access, nil).expect(t, 204)
	if list := s.do("GET", "/me/devices", ann.access, nil).expect(t, 200).items(t); len(list) != 0 {
		t.Fatalf("devices after unregistering = %v", list)
	}
}

func TestAdminTasksNotifyTheTeam(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	bo := s.register("Bo", "bo@example.com")
	cy := s.register("Cy", "cy@example.com")
	outsider := s.register("Di", "di@example.com")
	teamID := s.team(ann, "Platform")
	s.join(teamID, ann, bo, "bo@example.com", "admin")
	s.join(teamID, ann, cy, "cy@example.com", "member")
	for sess, tok := range map[session]string{ann: "ann-phone", bo: "bo-phone", cy: "cy-phone", outsider: "di-phone"} {
		s.device(sess, tok)
	}
	project := s.do("POST", "/projects", ann.access, map[string]any{"name": "Launch", "teamId": teamID}).expect(t, 201).str("id")

	// A member's task and a personal task notify nobody.
	s.do("POST", "/todos", cy.access, map[string]any{"title": "Draft notes", "projectId": project}).expect(t, 201)
	s.do("POST", "/todos", bo.access, map[string]any{"title": "Personal errand"}).expect(t, 201)
	if got := s.pushes.sent(); len(got) != 0 {
		t.Fatalf("pushes for member or personal tasks: %+v", got)
	}

	// An admin's task notifies the other members, not the admin or outsiders.
	todo := s.do("POST", "/todos", bo.access, map[string]any{"title": "Ship it", "projectId": project}).expect(t, 201).str("id")
	got := s.pushes.sent()
	if len(got) != 1 || !slices.Equal(got[0].tokens, []string{"ann-phone", "cy-phone"}) {
		t.Fatalf("pushes = %+v, want one to ann-phone and cy-phone", got)
	}
	m := got[0].msg
	if m.Title != "New task in Platform" || m.Body != "Bo added “Ship it”" || m.Data["todoId"] != todo || m.Data["teamId"] != teamID {
		t.Fatalf("message = %+v", m)
	}

	// The owner's tasks notify too; a removed member no longer hears about them.
	s.do("DELETE", "/teams/"+teamID+"/members/"+cy.userID, ann.access, nil).expect(t, 204)
	s.do("POST", "/todos", ann.access, map[string]any{"title": "Review", "projectId": project}).expect(t, 201)
	if got := s.pushes.sent(); len(got) != 2 || !slices.Equal(got[1].tokens, []string{"bo-phone"}) {
		t.Fatalf("owner's push = %+v, want only bo-phone", got)
	}
}

func TestNotificationsOffWithoutServiceAccount(t *testing.T) {
	s := newTestServer(t, noServiceAccount)
	ann := s.register("Ann", "ann@example.com")
	bo := s.register("Bo", "bo@example.com")
	teamID := s.team(ann, "Platform")
	s.join(teamID, ann, bo, "bo@example.com", "member")
	s.device(bo, "bo-phone") // registering still works
	project := s.do("POST", "/projects", ann.access, map[string]any{"name": "Launch", "teamId": teamID}).expect(t, 201).str("id")
	s.do("POST", "/todos", ann.access, map[string]any{"title": "Ship it", "projectId": project}).expect(t, 201)
	if got := s.pushes.sent(); len(got) != 0 {
		t.Fatalf("pushed without a service account: %+v", got)
	}
}

func TestAccountDeletionForgetsDevices(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	s.device(ann, "ann-phone")
	if list, _ := s.store.devices.ListForUser(context.Background(), ann.userID); len(list) != 1 {
		t.Fatalf("device not stored: %v", list)
	}
	s.do("DELETE", "/me", ann.access, nil).expect(t, 204)
	if list, err := s.store.devices.ListForUser(context.Background(), ann.userID); err != nil || len(list) != 0 {
		t.Fatalf("devices survived account deletion: %v, %v", list, err)
	}
}
