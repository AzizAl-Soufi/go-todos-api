// Package httpapi is the todos module's HTTP transport (driving adapter):
// /todos, /projects and /collections. Each endpoint decodes the request, runs
// one command and reads the response back with a query.
package httpapi

import (
	"net/http"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

type handler struct {
	cmd command.Handlers
	qry query.Handlers
}

// Register mounts the routes; protect must authenticate the caller and
// resolve them into an access.Actor. limitCreate runs after protect on
// POST /todos (a team task can notify the whole team).
func Register(
	mux *http.ServeMux,
	cmd command.Handlers,
	qry query.Handlers,
	protect httpx.Middleware,
	limitCreate httpx.Middleware,
) {
	h := handler{cmd: cmd, qry: qry}
	routes := map[string]func(r *http.Request, a access.Actor) (int, any, error){
		"POST /todos":        h.createTodo,
		"GET /todos":         h.listTodos,
		"GET /todos/{id}":    h.getTodo,
		"PUT /todos/{id}":    h.updateTodo, // kept for earlier clients; same as PATCH
		"PATCH /todos/{id}":  h.updateTodo,
		"DELETE /todos/{id}": h.deleteTodo,

		"POST /projects":        h.createProject,
		"GET /projects":         h.listProjects,
		"GET /projects/{id}":    h.getProject,
		"PATCH /projects/{id}":  h.updateProject,
		"DELETE /projects/{id}": h.deleteProject,

		"POST /collections":        h.createCollection,
		"GET /collections":         h.listCollections,
		"GET /collections/{id}":    h.getCollection,
		"PATCH /collections/{id}":  h.updateCollection,
		"DELETE /collections/{id}": h.deleteCollection,
	}
	for pattern, fn := range routes {
		var e http.Handler = access.ForActor(fn)
		if pattern == "POST /todos" {
			e = limitCreate(e)
		}
		mux.Handle(pattern, protect(e))
	}
}

// respond turns a read (query result, error) into an endpoint result.
func respond[T, R any](status int, v T, err error, toResponse func(T) R) (int, any, error) {
	if err != nil {
		return 0, nil, err
	}
	return status, toResponse(v), nil
}

// respondList is respond for slices.
func respondList[T, R any](items []T, err error, toResponse func(T) R) (int, any, error) {
	if err != nil {
		return 0, nil, err
	}
	out := make([]R, 0, len(items))
	for _, it := range items {
		out = append(out, toResponse(it))
	}
	return http.StatusOK, out, nil
}
