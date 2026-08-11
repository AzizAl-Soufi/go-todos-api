package v1

import (
	"net/http"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/shared/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/service"
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
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	todo, err := h.svc.CreateTodo(r.Context(), dto)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.RespondJSON(w, http.StatusCreated, todo)
}

func (h *TodosHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	todo, err := h.svc.GetTodo(r.Context(), idStr)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}

		httpx.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.RespondJSON(w, http.StatusOK, todo)
}

func (h *TodosHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	params, err := domain.ValidateUpdateTodoDTO(r)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	todo, err := h.svc.UpdateTodo(r.Context(), idStr, params)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.RespondJSON(w, http.StatusOK, todo)
}

func (h *TodosHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if err := h.svc.DeleteTodo(r.Context(), idStr); err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.RespondJSON(w, http.StatusOK, map[string]any{})
}

func (h *TodosHandler) GettAll(w http.ResponseWriter, r *http.Request) {
	todos, err := h.svc.GetTodos(r.Context())
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.RespondJSON(w, http.StatusOK, todos)
}
