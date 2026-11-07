package main

// End-to-end tests for organizing todos: todo fields, projects, collections,
// archiving, progress, filtering and sorting.

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// items decodes a JSON array response.
func (r response) items(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(r.raw), &out); err != nil {
		t.Fatalf("not a JSON array: %s", r.raw)
	}
	return out
}

func titles(items []map[string]any) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it["title"].(string))
	}
	return out
}

func progressOf(t *testing.T, v any) map[string]float64 {
	t.Helper()
	m, ok := v.(map[string]any)["progress"].(map[string]any)
	if !ok {
		t.Fatalf("no progress in %v", v)
	}
	out := map[string]float64{}
	for k, n := range m {
		out[k] = n.(float64)
	}
	return out
}

func TestTodoFields(t *testing.T) {
	s := newTestServer(t)
	u := s.register("Jo", "jo@example.com")

	due := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	r := s.do("POST", "/todos", u.access, map[string]any{
		"title": "Pay rent", "notes": "IBAN in email", "status": "in_progress", "priority": "urgent",
		"tags": []string{"money", " money ", "home"}, "dueAt": due.Format(time.RFC3339Nano),
	}).expect(t, 201)
	if r.str("status") != "in_progress" || r.str("priority") != "urgent" || r.str("notes") != "IBAN in email" ||
		r.body["overdue"] != true || r.body["completed"] != false || r.body["projectId"] != nil || r.body["completedAt"] != nil {
		t.Fatalf("created todo: %s", r.raw)
	}
	if tags := r.body["tags"].([]any); len(tags) != 2 {
		t.Fatalf("tags must be trimmed and deduplicated: %v", tags)
	}
	if got, _ := time.Parse(time.RFC3339, r.str("dueAt")); !got.Equal(due) {
		t.Fatalf("dueAt = %s, want %s", r.str("dueAt"), due)
	}
	id := r.str("id")

	r = s.do("PATCH", "/todos/"+id, u.access, map[string]any{"status": "done"}).expect(t, 200)
	if r.body["completed"] != true || r.body["completedAt"] == nil || r.body["overdue"] != false {
		t.Fatalf("done todo: %s", r.raw)
	}
	r = s.do("PATCH", "/todos/"+id, u.access, map[string]any{"completed": false, "dueAt": nil, "tags": []string{}}).expect(t, 200)
	if r.str("status") != "pending" || r.body["completedAt"] != nil || r.body["dueAt"] != nil || len(r.body["tags"].([]any)) != 0 {
		t.Fatalf("reopened todo: %s", r.raw)
	}
	s.do("PATCH", "/todos/"+id, u.access, map[string]any{"dueAt": "2026-12-24"}).expect(t, 200)

	for body, code := range map[string]string{
		`{"status": "later"}`:      "INVALID_STATUS",
		`{"status": null}`:         "INVALID_STATUS",
		`{"priority": "critical"}`: "INVALID_PRIORITY",
		`{"dueAt": "tomorrow"}`:    "INVALID_FIELD",
		`{"tags": "home"}`:         "INVALID_FIELD",
		`{"tags": [""]}`:           "INVALID_TAG",
		`{"notes": 5}`:             "INVALID_FIELD",
	} {
		var b map[string]any
		json.Unmarshal([]byte(body), &b)
		s.do("PATCH", "/todos/"+id, u.access, b).expectCode(t, 400, code)
	}
}

func TestProjectsAndCollections(t *testing.T) {
	s := newTestServer(t)
	u := s.register("Kai", "kai@example.com")
	other := s.register("Lu", "lu@example.com")

	col := s.do("POST", "/collections", u.access, map[string]any{"name": "Work", "icon": "💼", "color": "blue"}).expect(t, 201)
	colID := col.str("id")
	if col.str("color") != "blue" || col.body["archived"] != false || len(col.body["projects"].([]any)) != 0 {
		t.Fatalf("collection: %s", col.raw)
	}
	proj := s.do("POST", "/projects", u.access, map[string]any{"name": "Launch", "collectionId": colID}).expect(t, 201)
	projID := proj.str("id")
	if proj.str("collectionId") != colID || proj.str("color") != "default" || progressOf(t, proj.body)["total"] != 0 {
		t.Fatalf("project: %s", proj.raw)
	}

	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	for _, todo := range []map[string]any{
		{"title": "Plan", "projectId": projID, "status": "done"},
		{"title": "Build", "projectId": projID, "status": "in_progress", "dueAt": past},
		{"title": "Cut scope", "projectId": projID, "status": "cancelled"},
		{"title": "Inbox item"},
	} {
		s.do("POST", "/todos", u.access, todo).expect(t, 201)
	}

	p := progressOf(t, s.do("GET", "/projects/"+projID, u.access, nil).expect(t, 200).body)
	if p["total"] != 3 || p["done"] != 1 || p["inProgress"] != 1 || p["cancelled"] != 1 || p["overdue"] != 1 || p["percentDone"] != 50 {
		t.Fatalf("project progress = %v", p)
	}
	c := s.do("GET", "/collections/"+colID, u.access, nil).expect(t, 200)
	if progressOf(t, c.body)["total"] != 3 || len(c.body["projects"].([]any)) != 1 {
		t.Fatalf("collection view: %s", c.raw)
	}

	if got := titles(s.do("GET", "/todos?project="+projID, u.access, nil).expect(t, 200).items(t)); len(got) != 3 {
		t.Fatalf("project todos = %q", got)
	}
	if got := titles(s.do("GET", "/todos?project=inbox", u.access, nil).expect(t, 200).items(t)); !slices.Equal(got, []string{"Inbox item"}) {
		t.Fatalf("inbox = %q", got)
	}

	// Other users can't see or use the project or collection.
	s.do("GET", "/projects/"+projID, other.access, nil).expectCode(t, 404, "PROJECT_NOT_FOUND")
	s.do("POST", "/todos", other.access, map[string]any{"title": "x", "projectId": projID}).expectCode(t, 404, "PROJECT_NOT_FOUND")
	s.do("POST", "/projects", other.access, map[string]any{"name": "x", "collectionId": colID}).expectCode(t, 404, "COLLECTION_NOT_FOUND")
	s.do("GET", "/todos?project="+projID, other.access, nil).expectCode(t, 404, "PROJECT_NOT_FOUND")
	s.do("POST", "/todos", u.access, map[string]any{"title": "x", "projectId": "not-an-id"}).expectCode(t, 404, "PROJECT_NOT_FOUND")

	// Archiving hides the project and its todos by default.
	s.do("PATCH", "/projects/"+projID, u.access, map[string]any{"archived": true}).expect(t, 200)
	if got := s.do("GET", "/projects", u.access, nil).expect(t, 200).items(t); len(got) != 0 {
		t.Fatalf("archived project listed: %v", got)
	}
	if got := s.do("GET", "/projects?archived=true", u.access, nil).expect(t, 200).items(t); len(got) != 1 || got[0]["archived"] != true {
		t.Fatalf("archived projects = %v", got)
	}
	if got := titles(s.do("GET", "/todos", u.access, nil).expect(t, 200).items(t)); !slices.Equal(got, []string{"Inbox item"}) {
		t.Fatalf("todos of archived projects must be hidden: %q", got)
	}
	if got := s.do("GET", "/todos?include_archived=true", u.access, nil).expect(t, 200).items(t); len(got) != 4 {
		t.Fatalf("include_archived: %d todos", len(got))
	}
	if got := s.do("GET", "/todos?project="+projID, u.access, nil).expect(t, 200).items(t); len(got) != 3 {
		t.Fatal("asking for an archived project explicitly still lists its todos")
	}
	if c := s.do("GET", "/collections/"+colID, u.access, nil).expect(t, 200); len(c.body["projects"].([]any)) != 0 {
		t.Fatalf("collections show active projects only: %s", c.raw)
	}
	s.do("PATCH", "/projects/"+projID, u.access, map[string]any{"archived": false}).expect(t, 200)

	// Moving a todo between projects and the inbox.
	inbox := s.do("GET", "/todos?project=inbox", u.access, nil).items(t)[0]["id"].(string)
	s.do("PATCH", "/todos/"+inbox, u.access, map[string]any{"projectId": projID}).expect(t, 200)
	if r := s.do("PATCH", "/todos/"+inbox, u.access, map[string]any{"projectId": nil}).expect(t, 200); r.body["projectId"] != nil {
		t.Fatalf("projectId null must move the todo to the inbox: %s", r.raw)
	}

	// Deleting a collection keeps its projects; deleting a project deletes its todos.
	s.do("DELETE", "/collections/"+colID, u.access, nil).expect(t, 204)
	s.do("GET", "/collections/"+colID, u.access, nil).expectCode(t, 404, "COLLECTION_NOT_FOUND")
	if r := s.do("GET", "/projects/"+projID, u.access, nil).expect(t, 200); r.body["collectionId"] != nil {
		t.Fatalf("project still filed under a deleted collection: %s", r.raw)
	}
	s.do("DELETE", "/projects/"+projID, u.access, nil).expect(t, 204)
	s.do("DELETE", "/projects/"+projID, u.access, nil).expectCode(t, 404, "PROJECT_NOT_FOUND")
	if got := titles(s.do("GET", "/todos?include_archived=true", u.access, nil).expect(t, 200).items(t)); !slices.Equal(got, []string{"Inbox item"}) {
		t.Fatalf("todos left after deleting their project: %q", got)
	}

	// Validation.
	s.do("POST", "/projects", u.access, map[string]any{"name": " "}).expectCode(t, 400, "INVALID_NAME")
	s.do("POST", "/collections", u.access, map[string]any{"name": "x", "color": "teal"}).expectCode(t, 400, "INVALID_COLOR")
	s.do("PATCH", "/collections/"+colID, u.access, map[string]any{}).expectCode(t, 400, "INVALID_BODY")
	s.do("GET", "/projects?archived=maybe", u.access, nil).expectCode(t, 400, "INVALID_QUERY")
}

func TestTodoFiltersAndSorting(t *testing.T) {
	s := newTestServer(t)
	u := s.register("Mo", "mo@example.com")
	day := func(d int) string { return time.Now().UTC().AddDate(0, 0, d).Format(time.DateOnly) }
	for _, todo := range []map[string]any{
		{"title": "A", "priority": "low", "dueAt": day(3), "tags": []string{"x"}},
		{"title": "B", "priority": "urgent"},
		{"title": "C", "priority": "high", "dueAt": day(1), "status": "blocked"},
		{"title": "D", "dueAt": day(-1), "notes": "Overdue thing"},
		{"title": "E", "dueAt": day(-2), "status": "done"},
	} {
		s.do("POST", "/todos", u.access, todo).expect(t, 201)
		time.Sleep(2 * time.Millisecond) // distinct creation times for newest-first
	}

	for query, want := range map[string]string{
		"":                                  "E D C B A",
		"sort=due":                          "E D C A B",
		"sort=priority":                     "B C A E D",
		"status=pending,blocked&sort=due":   "D C A B",
		"priority=high,urgent":              "C B",
		"tag=x":                             "A",
		"overdue=true":                      "D",
		"q=overdue":                         "D",
		"due_before=" + day(0):              "E D",
		"due_after=" + day(0) + "&sort=due": "C A",
	} {
		got := strings.Join(titles(s.do("GET", "/todos?"+query, u.access, nil).expect(t, 200).items(t)), " ")
		if got != want {
			t.Errorf("GET /todos?%s = %q, want %q", query, got, want)
		}
	}

	for query, code := range map[string]string{
		"status=later":      "INVALID_STATUS",
		"priority=critical": "INVALID_PRIORITY",
		"sort=title":        "INVALID_SORT",
		"overdue=maybe":     "INVALID_QUERY",
		"due_before=" + url.QueryEscape("next week"): "INVALID_QUERY",
	} {
		s.do("GET", "/todos?"+query, u.access, nil).expectCode(t, 400, code)
	}
}

func TestAccountDeletionRemovesProjectsAndCollections(t *testing.T) {
	s := newTestServer(t)
	u := s.register("Ny", "ny@example.com")
	col := s.do("POST", "/collections", u.access, map[string]any{"name": "C"}).expect(t, 201).str("id")
	proj := s.do("POST", "/projects", u.access, map[string]any{"name": "P", "collectionId": col}).expect(t, 201).str("id")
	s.do("POST", "/todos", u.access, map[string]any{"title": "t", "projectId": proj}).expect(t, 201)

	s.do("DELETE", "/me", u.access, nil).expect(t, 204)
	again := s.register("Ny", "ny@example.com")
	for _, path := range []string{"/todos?include_archived=true", "/projects?archived=all", "/collections?archived=all"} {
		if got := s.do("GET", path, again.access, nil).expect(t, 200).items(t); len(got) != 0 {
			t.Fatalf("%s after account deletion: %v", path, got)
		}
	}
}
