package main

// End-to-end tests for teams: invitations, roles, team-owned content and the
// audit logs.

import (
	"slices"
	"testing"
)

// team creates a team owned by owner and returns its ID.
func (s *testServer) team(owner session, name string) string {
	s.t.Helper()
	r := s.do("POST", "/teams", owner.access, map[string]string{"name": name}).expect(s.t, 201)
	if r.str("role") != "owner" {
		s.t.Fatalf("creator must be the owner: %s", r.raw)
	}
	return r.str("id")
}

// join invites email with role (as inviter) and accepts as invitee.
func (s *testServer) join(teamID string, inviter, invitee session, email, role string) {
	s.t.Helper()
	s.do("POST", "/teams/"+teamID+"/invitations", inviter.access, map[string]string{"email": email, "role": role}).expect(s.t, 201)
	s.do("POST", "/invitations/accept", invitee.access, map[string]string{"token": s.mailer.inviteToken(s.t, email)}).expect(s.t, 200)
}

func memberRoles(t *testing.T, r response) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range r.body["members"].([]any) {
		mm := m.(map[string]any)
		out[mm["email"].(string)] = mm["role"].(string)
	}
	return out
}

func TestTeamInvitations(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	bo := s.register("Bo", "bo@example.com")
	cy := s.register("Cy", "cy@example.com")
	eve := s.register("Eve", "eve@example.com")

	teamID := s.team(ann, " Platform ")
	r := s.do("POST", "/teams/"+teamID+"/invitations", ann.access, map[string]string{"email": "Bo@Example.com", "role": "member"}).expect(t, 201)
	if r.str("email") != "bo@example.com" || r.str("role") != "member" || r.body["expired"] != false || r.str("invitedBy") != ann.userID {
		t.Fatalf("invitation: %s", r.raw)
	}
	bobToken := s.mailer.inviteToken(t, "bo@example.com")

	// The token only works for the invited email, and only once.
	s.do("POST", "/invitations/accept", eve.access, map[string]string{"token": bobToken}).expectCode(t, 403, "INVITATION_EMAIL_MISMATCH")
	r = s.do("POST", "/invitations/accept", bo.access, map[string]string{"token": bobToken}).expect(t, 200)
	if r.str("name") != "Platform" || r.str("role") != "member" {
		t.Fatalf("accepted team: %s", r.raw)
	}
	s.do("POST", "/invitations/accept", bo.access, map[string]string{"token": bobToken}).expectCode(t, 400, "INVALID_INVITATION_TOKEN")
	s.do("POST", "/invitations/accept", bo.access, map[string]string{"token": ""}).expectCode(t, 400, "INVALID_INVITATION_TOKEN")

	if roles := memberRoles(t, s.do("GET", "/teams/"+teamID, bo.access, nil).expect(t, 200)); roles["ann@example.com"] != "owner" || roles["bo@example.com"] != "member" {
		t.Fatalf("members: %v", roles)
	}
	if list := s.do("GET", "/teams", bo.access, nil).expect(t, 200).items(t); len(list) != 1 || list[0]["role"] != "member" {
		t.Fatalf("Bo's teams: %v", list)
	}

	// Rules: members can't invite, admins can't invite admins, nobody invites
	// an owner or an existing member.
	invite := func(by session, email, role string) response {
		return s.do("POST", "/teams/"+teamID+"/invitations", by.access, map[string]string{"email": email, "role": role})
	}
	invite(bo, "cy@example.com", "viewer").expectCode(t, 403, "INSUFFICIENT_ROLE")
	invite(ann, "cy@example.com", "owner").expectCode(t, 400, "OWNER_NOT_ASSIGNABLE")
	invite(ann, "bo@example.com", "viewer").expectCode(t, 409, "ALREADY_MEMBER")
	invite(ann, "not-an-email", "viewer").expectCode(t, 400, "INVALID_EMAIL")
	invite(ann, "cy@example.com", "boss").expectCode(t, 400, "INVALID_ROLE")

	// Re-inviting replaces the pending invitation; admins see and revoke them.
	invite(ann, "cy@example.com", "viewer").expect(t, 201)
	first := s.mailer.inviteToken(t, "cy@example.com")
	invite(ann, "cy@example.com", "member").expect(t, 201)
	pending := s.do("GET", "/teams/"+teamID+"/invitations", ann.access, nil).expect(t, 200).items(t)
	if len(pending) != 1 || pending[0]["role"] != "member" {
		t.Fatalf("pending: %v", pending)
	}
	s.do("GET", "/teams/"+teamID+"/invitations", bo.access, nil).expectCode(t, 403, "INSUFFICIENT_ROLE")
	s.do("POST", "/invitations/accept", cy.access, map[string]string{"token": first}).expectCode(t, 400, "INVALID_INVITATION_TOKEN")

	s.do("DELETE", "/teams/"+teamID+"/invitations/"+pending[0]["id"].(string), ann.access, nil).expect(t, 204)
	s.do("POST", "/invitations/accept", cy.access, map[string]string{"token": s.mailer.inviteToken(t, "cy@example.com")}).
		expectCode(t, 400, "INVALID_INVITATION_TOKEN")

	invite(ann, "cy@example.com", "viewer").expect(t, 201)
	s.do("POST", "/invitations/decline", cy.access, map[string]string{"token": s.mailer.inviteToken(t, "cy@example.com")}).expect(t, 204)
	if pending := s.do("GET", "/teams/"+teamID+"/invitations", ann.access, nil).expect(t, 200).items(t); len(pending) != 0 {
		t.Fatalf("declined invitation still pending: %v", pending)
	}

	// Outsiders can't tell the team exists.
	s.do("GET", "/teams/"+teamID, eve.access, nil).expectCode(t, 404, "TEAM_NOT_FOUND")
	s.do("GET", "/teams/not-an-id", eve.access, nil).expectCode(t, 404, "TEAM_NOT_FOUND")
	s.do("PATCH", "/teams/"+teamID, eve.access, map[string]string{"name": "x"}).expectCode(t, 404, "TEAM_NOT_FOUND")
	s.do("GET", "/teams", "", nil).expectCode(t, 401, "MISSING_ACCESS_TOKEN")
}

func TestTeamRoles(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	bo := s.register("Bo", "bo@example.com")
	cy := s.register("Cy", "cy@example.com")
	teamID := s.team(ann, "Platform")
	s.join(teamID, ann, bo, "bo@example.com", "member")
	s.join(teamID, ann, cy, "cy@example.com", "viewer")

	role := func(by session, user session, role string) response {
		return s.do("PATCH", "/teams/"+teamID+"/members/"+user.userID, by.access, map[string]string{"role": role})
	}
	role(bo, cy, "member").expectCode(t, 403, "INSUFFICIENT_ROLE")
	role(ann, ann, "admin").expectCode(t, 409, "CANNOT_CHANGE_OWN_ROLE")
	role(ann, bo, "owner").expectCode(t, 400, "OWNER_NOT_ASSIGNABLE")
	if roles := memberRoles(t, role(ann, bo, "admin").expect(t, 200)); roles["bo@example.com"] != "admin" {
		t.Fatalf("promotion not saved: %v", roles)
	}
	// An admin manages people below them only, and grants roles below their own.
	role(bo, cy, "admin").expectCode(t, 403, "INSUFFICIENT_ROLE")
	role(bo, ann, "member").expectCode(t, 403, "INSUFFICIENT_ROLE")
	role(bo, cy, "member").expect(t, 200)
	s.do("PATCH", "/teams/"+teamID, bo.access, map[string]string{"name": "Core"}).expect(t, 200)
	s.do("PATCH", "/teams/"+teamID, cy.access, map[string]string{"name": "Mine"}).expectCode(t, 403, "INSUFFICIENT_ROLE")

	// Leaving and removing.
	s.do("DELETE", "/teams/"+teamID+"/members/"+ann.userID, ann.access, nil).expectCode(t, 409, "OWNER_CANNOT_LEAVE")
	s.do("DELETE", "/teams/"+teamID+"/members/"+ann.userID, bo.access, nil).expectCode(t, 403, "INSUFFICIENT_ROLE")
	s.do("DELETE", "/teams/"+teamID+"/members/"+cy.userID, cy.access, nil).expect(t, 204)
	s.do("GET", "/teams/"+teamID, cy.access, nil).expectCode(t, 404, "TEAM_NOT_FOUND")
	s.do("DELETE", "/teams/"+teamID+"/members/"+cy.userID, ann.access, nil).expectCode(t, 404, "MEMBER_NOT_FOUND")

	// Ownership moves to another member; the old owner becomes an admin.
	s.do("POST", "/teams/"+teamID+"/transfer-ownership", bo.access, map[string]string{"userId": ann.userID}).expectCode(t, 403, "INSUFFICIENT_ROLE")
	s.do("POST", "/teams/"+teamID+"/transfer-ownership", ann.access, map[string]string{"userId": ann.userID}).expectCode(t, 409, "CANNOT_TRANSFER_TO_SELF")
	s.do("POST", "/teams/"+teamID+"/transfer-ownership", ann.access, map[string]string{"userId": cy.userID}).expectCode(t, 404, "MEMBER_NOT_FOUND")
	r := s.do("POST", "/teams/"+teamID+"/transfer-ownership", ann.access, map[string]string{"userId": bo.userID}).expect(t, 200)
	if roles := memberRoles(t, r); roles["bo@example.com"] != "owner" || roles["ann@example.com"] != "admin" {
		t.Fatalf("after transfer: %v", roles)
	}
	s.do("DELETE", "/teams/"+teamID, ann.access, nil).expectCode(t, 403, "INSUFFICIENT_ROLE")
	s.do("DELETE", "/teams/"+teamID, bo.access, nil).expect(t, 204)
	s.do("GET", "/teams/"+teamID, bo.access, nil).expectCode(t, 404, "TEAM_NOT_FOUND")
}

func TestTeamContent(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	bo := s.register("Bo", "bo@example.com")
	vi := s.register("Vi", "vi@example.com")
	eve := s.register("Eve", "eve@example.com")
	teamID := s.team(ann, "Platform")
	s.join(teamID, ann, bo, "bo@example.com", "member")
	s.join(teamID, ann, vi, "vi@example.com", "viewer")

	// Team collections and projects belong to the team; members create them.
	coll := s.do("POST", "/collections", bo.access, map[string]any{"name": "Roadmap", "teamId": teamID}).expect(t, 201)
	if coll.str("teamId") != teamID || coll.str("userId") != bo.userID {
		t.Fatalf("team collection: %s", coll.raw)
	}
	proj := s.do("POST", "/projects", bo.access, map[string]any{"name": "Launch", "teamId": teamID, "collectionId": coll.str("id")}).expect(t, 201)
	projectID := proj.str("id")
	personal := s.do("POST", "/projects", bo.access, map[string]any{"name": "Mine"}).expect(t, 201)
	if personal.body["teamId"] != nil {
		t.Fatalf("a project without teamId is personal: %s", personal.raw)
	}

	s.do("POST", "/projects", bo.access, map[string]any{"name": "x", "teamId": teamID, "collectionId": "not-an-id"}).expectCode(t, 404, "COLLECTION_NOT_FOUND")
	personalColl := s.do("POST", "/collections", bo.access, map[string]any{"name": "Private"}).expect(t, 201)
	s.do("POST", "/projects", bo.access, map[string]any{"name": "x", "teamId": teamID, "collectionId": personalColl.str("id")}).expectCode(t, 400, "TEAM_MISMATCH")
	s.do("PATCH", "/projects/"+projectID, bo.access, map[string]any{"teamId": nil}).expectCode(t, 400, "TEAM_IMMUTABLE")
	s.do("POST", "/projects", eve.access, map[string]any{"name": "x", "teamId": teamID}).expectCode(t, 404, "TEAM_NOT_FOUND")
	s.do("POST", "/projects", vi.access, map[string]any{"name": "x", "teamId": teamID}).expectCode(t, 403, "INSUFFICIENT_ROLE")

	// A todo in a team project belongs to the team.
	todo := s.do("POST", "/todos", bo.access, map[string]any{"title": "Ship it", "projectId": projectID}).expect(t, 201)
	todoID := todo.str("id")
	if todo.str("teamId") != teamID {
		t.Fatalf("team todo: %s", todo.raw)
	}
	s.do("POST", "/todos", bo.access, map[string]any{"title": "My errand"}).expect(t, 201)

	// Every member sees team content; viewers can't change it; outsiders can't see it.
	s.do("GET", "/todos/"+todoID, vi.access, nil).expect(t, 200)
	s.do("PATCH", "/todos/"+todoID, vi.access, map[string]any{"status": "done"}).expectCode(t, 403, "INSUFFICIENT_ROLE")
	s.do("POST", "/todos", vi.access, map[string]any{"title": "x", "projectId": projectID}).expectCode(t, 403, "INSUFFICIENT_ROLE")
	s.do("GET", "/todos/"+todoID, eve.access, nil).expectCode(t, 404, "NOT_FOUND")
	s.do("GET", "/projects/"+projectID, eve.access, nil).expectCode(t, 404, "PROJECT_NOT_FOUND")
	s.do("PATCH", "/todos/"+todoID, ann.access, map[string]any{"status": "in_progress"}).expect(t, 200)

	r := s.do("GET", "/projects/"+projectID, vi.access, nil).expect(t, 200)
	if p := progressOf(t, r.body); p["total"] != 1 || p["inProgress"] != 1 {
		t.Fatalf("team project progress for a viewer: %v", p)
	}

	// Listings mix personal and team records; ?team= narrows them.
	if got := titles(s.do("GET", "/todos", bo.access, nil).expect(t, 200).items(t)); !slices.Equal(got, []string{"My errand", "Ship it"}) {
		t.Fatalf("Bo's todos: %q", got)
	}
	if got := titles(s.do("GET", "/todos?team="+teamID, bo.access, nil).expect(t, 200).items(t)); !slices.Equal(got, []string{"Ship it"}) {
		t.Fatalf("team todos: %q", got)
	}
	if got := titles(s.do("GET", "/todos?team=personal", bo.access, nil).expect(t, 200).items(t)); !slices.Equal(got, []string{"My errand"}) {
		t.Fatalf("personal todos: %q", got)
	}
	if got := titles(s.do("GET", "/todos", ann.access, nil).expect(t, 200).items(t)); !slices.Equal(got, []string{"Ship it"}) {
		t.Fatalf("Ann sees team todos but not Bo's own: %q", got)
	}
	s.do("GET", "/todos?team="+teamID, eve.access, nil).expectCode(t, 404, "TEAM_NOT_FOUND")
	if list := s.do("GET", "/projects?team="+teamID, vi.access, nil).expect(t, 200).items(t); len(list) != 1 || list[0]["name"] != "Launch" {
		t.Fatalf("team projects: %v", list)
	}
	if list := s.do("GET", "/collections?team=personal", bo.access, nil).expect(t, 200).items(t); len(list) != 1 || list[0]["name"] != "Private" {
		t.Fatalf("personal collections: %v", list)
	}
	r = s.do("GET", "/collections/"+coll.str("id"), vi.access, nil).expect(t, 200)
	if len(r.body["projects"].([]any)) != 1 {
		t.Fatalf("team collection projects: %s", r.raw)
	}

	// Members can't delete team projects or collections; admins and owners can.
	s.do("DELETE", "/projects/"+projectID, bo.access, nil).expectCode(t, 403, "INSUFFICIENT_ROLE")
	s.do("DELETE", "/collections/"+coll.str("id"), bo.access, nil).expectCode(t, 403, "INSUFFICIENT_ROLE")

	// Moving a team todo to the inbox makes it the mover's personal todo.
	moved := s.do("PATCH", "/todos/"+todoID, bo.access, map[string]any{"projectId": nil}).expect(t, 200)
	if moved.body["teamId"] != nil || moved.str("userId") != bo.userID {
		t.Fatalf("moved todo: %s", moved.raw)
	}
	s.do("GET", "/todos/"+todoID, ann.access, nil).expectCode(t, 404, "NOT_FOUND")
	s.do("PATCH", "/todos/"+todoID, bo.access, map[string]any{"projectId": projectID}).expect(t, 200)
	s.do("GET", "/todos/"+todoID, ann.access, nil).expect(t, 200)

	// Losing membership loses access at once (memberships are read per request).
	s.do("DELETE", "/teams/"+teamID+"/members/"+vi.userID, ann.access, nil).expect(t, 204)
	s.do("GET", "/projects/"+projectID, vi.access, nil).expectCode(t, 404, "PROJECT_NOT_FOUND")

	s.do("DELETE", "/projects/"+projectID, ann.access, nil).expect(t, 204)
	s.do("GET", "/todos/"+todoID, bo.access, nil).expectCode(t, 404, "NOT_FOUND")
	s.do("DELETE", "/collections/"+coll.str("id"), ann.access, nil).expect(t, 204)

	// Deleting the team deletes its content; personal records stay.
	again := s.do("POST", "/projects", bo.access, map[string]any{"name": "Again", "teamId": teamID}).expect(t, 201)
	s.do("DELETE", "/teams/"+teamID, ann.access, nil).expect(t, 204)
	s.do("GET", "/projects/"+again.str("id"), ann.access, nil).expectCode(t, 404, "PROJECT_NOT_FOUND")
	if got := titles(s.do("GET", "/todos", bo.access, nil).expect(t, 200).items(t)); !slices.Equal(got, []string{"My errand"}) {
		t.Fatalf("Bo's todos after team deletion: %q", got)
	}
}

func actions(t *testing.T, r response) []string {
	t.Helper()
	out := []string{}
	for _, e := range r.body["events"].([]any) {
		out = append(out, e.(map[string]any)["action"].(string))
	}
	return out
}

func TestAuditLogs(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	bo := s.register("Bo", "bo@example.com")
	teamID := s.team(ann, "Platform")
	s.join(teamID, ann, bo, "bo@example.com", "member")
	proj := s.do("POST", "/projects", bo.access, map[string]any{"name": "Launch", "teamId": teamID}).expect(t, 201)
	todo := s.do("POST", "/todos", bo.access, map[string]any{"title": "Ship it", "projectId": proj.str("id")}).expect(t, 201)
	s.do("PATCH", "/todos/"+todo.str("id"), bo.access, map[string]any{"status": "done", "priority": "high"}).expect(t, 200)
	s.do("PATCH", "/teams/"+teamID+"/members/"+bo.userID, ann.access, map[string]string{"role": "viewer"}).expect(t, 200)
	s.do("POST", "/todos", bo.access, map[string]any{"title": "Personal"}).expect(t, 201) // not audited

	r := s.do("GET", "/teams/"+teamID+"/audit-log", ann.access, nil).expect(t, 200)
	want := []string{"member.role_changed", "todo.updated", "todo.created", "project.created", "member.joined", "invitation.created", "team.created"}
	if got := actions(t, r); !slices.Equal(got, want) {
		t.Fatalf("team log = %q\nwant %q", got, want)
	}
	updated := r.body["events"].([]any)[1].(map[string]any)
	if updated["actorId"] != bo.userID || updated["targetId"] != todo.str("id") || updated["teamId"] != teamID ||
		updated["details"].(map[string]any)["fields"] != "priority,status" || updated["ip"] == "" {
		t.Fatalf("todo.updated event: %v", updated)
	}
	if r.body["nextCursor"] != nil {
		t.Fatalf("one page only: %s", r.raw)
	}

	// Pages follow the cursor.
	page1 := s.do("GET", "/teams/"+teamID+"/audit-log?limit=3", ann.access, nil).expect(t, 200)
	cursor := page1.str("nextCursor")
	page2 := s.do("GET", "/teams/"+teamID+"/audit-log?limit=3&cursor="+cursor, ann.access, nil).expect(t, 200)
	if got := append(actions(t, page1), actions(t, page2)...); !slices.Equal(got, want[:6]) || page2.str("nextCursor") == "" {
		t.Fatalf("pages = %q (next %q)", got, page2.str("nextCursor"))
	}
	s.do("GET", "/teams/"+teamID+"/audit-log?cursor=garbage", ann.access, nil).expectCode(t, 400, "INVALID_CURSOR")
	s.do("GET", "/teams/"+teamID+"/audit-log?limit=1000", ann.access, nil).expectCode(t, 400, "INVALID_LIMIT")
	s.do("GET", "/teams/"+teamID+"/audit-log", bo.access, nil).expectCode(t, 403, "INSUFFICIENT_ROLE")

	// The personal security log.
	s.login("bo@example.com")
	r = s.do("GET", "/me/audit-log", bo.access, nil).expect(t, 200)
	if got := actions(t, r); !slices.Equal(got, []string{"session.login", "user.registered"}) {
		t.Fatalf("security log = %q", got)
	}
	login := r.body["events"].([]any)[0].(map[string]any)
	if login["actorId"] != bo.userID || login["details"].(map[string]any)["provider"] != "password" {
		t.Fatalf("login event: %v", login)
	}
	if got := actions(t, s.do("GET", "/me/audit-log", ann.access, nil).expect(t, 200)); !slices.Equal(got, []string{"user.registered"}) {
		t.Fatalf("Ann's security log = %q", got)
	}
}

func TestDeleteAccountWithTeams(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	bo := s.register("Bo", "bo@example.com")
	shared := s.team(ann, "Shared")
	solo := s.team(ann, "Solo")
	s.join(shared, ann, bo, "bo@example.com", "member")
	proj := s.do("POST", "/projects", bo.access, map[string]any{"name": "Launch", "teamId": shared}).expect(t, 201)
	todo := s.do("POST", "/todos", bo.access, map[string]any{"title": "Bo's team todo", "projectId": proj.str("id")}).expect(t, 201)

	// An owner of a team with other members must hand it over first.
	s.do("DELETE", "/me", ann.access, nil).expectCode(t, 409, "OWNS_TEAMS")
	s.do("GET", "/teams/"+solo, ann.access, nil).expect(t, 200) // nothing was deleted

	// A member's account deletion leaves the team; their team todos stay.
	s.do("DELETE", "/me", bo.access, nil).expect(t, 204)
	if roles := memberRoles(t, s.do("GET", "/teams/"+shared, ann.access, nil).expect(t, 200)); len(roles) != 1 {
		t.Fatalf("Bo is still a member: %v", roles)
	}
	if r := s.do("GET", "/todos/"+todo.str("id"), ann.access, nil).expect(t, 200); r.str("userId") != bo.userID {
		t.Fatalf("team todo keeps its creator: %s", r.raw)
	}

	// Now Ann owns teams without other members: they go with the account.
	s.do("DELETE", "/me", ann.access, nil).expect(t, 204)
	carl := s.register("Carl", "carl@example.com")
	s.do("GET", "/teams/"+solo, carl.access, nil).expectCode(t, 404, "TEAM_NOT_FOUND")
}
