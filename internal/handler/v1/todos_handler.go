package v1

import (
	"net/http"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/service"
	"github.com/AzizAl-Soufi/go-todos-api/internal/shared/httpx"
)

type TodosHandler struct {
	svc service.TodosService
}

func NewTodosHandler(svc service.TodosService) *TodosHandler {
	return &TodosHandler{svc: svc}
}

func (h *TodosHandler) Create(w http.ResponseWriter, r *http.Request) {
	dto, err := domain.ValidateCreateDTO(r)
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	todo, err := h.svc.CreateTodo(r.Context(), dto)
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	httpx.RespondJSON(w, http.StatusCreated, todo)
}

func (h *TodosHandler) Get(w http.ResponseWriter, r *http.Request) {
	todo, err := h.svc.GetTodo(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	httpx.RespondJSON(w, http.StatusOK, todo)
}

func (h *TodosHandler) Update(w http.ResponseWriter, r *http.Request) {
	params, err := domain.ValidateUpdateTodoDTO(r)
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	todo, err := h.svc.UpdateTodo(r.Context(), r.PathValue("id"), params)
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	httpx.RespondJSON(w, http.StatusOK, todo)
}

func (h *TodosHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteTodo(r.Context(), r.PathValue("id")); err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	httpx.RespondJSON(w, http.StatusOK, map[string]any{})
}

// GetAll lists the authenticated user's todos.
func (h *TodosHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	todos, err := h.svc.GetTodos(r.Context())
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	httpx.RespondJSON(w, http.StatusOK, todos)
}
