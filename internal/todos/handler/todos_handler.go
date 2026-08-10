package handler

import (
	"errors"
	"net/http"

	common "github.com/AzizAl-Soufi/todos-api/internal/common"
	"github.com/AzizAl-Soufi/todos-api/internal/common/errors"
	"github.com/AzizAl-Soufi/todos-api/internal/common/middleware"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/service"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type TodosHandler struct {
	svc service.TodosService
}

func NewTodosHandler(svc service.TodosService) *TodosHandler {
	return &TodosHandler{svc: svc}
}

func (h *TodosHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetAuthorization(r.Context())
	if !ok {
		err := middleware.ErrUnauthorized
		common.RespondError(w, err.Status(), err.Error())
		return
	}

	dto, err := domain.ValidateCreateDTO(r)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			common.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		common.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	todo, err := h.svc.CreateTodo(r.Context(), claims.ID, dto)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			common.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		common.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	common.RespondJSON(w, http.StatusCreated, todo)
}

func (h *TodosHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			common.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		if errors.Is(err, bson.ErrInvalidHex) {
			common.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		common.RespondError(w, http.StatusInternalServerError, err.Error())
	}

	claims, ok := middleware.GetAuthorization(r.Context())
	if !ok {
		err := middleware.ErrUnauthorized
		common.RespondError(w, err.Status(), err.Error())
		return
	}

	todo, err := h.svc.GetTodo(r.Context(), id, claims.ID)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			common.RespondError(w, appErr.Status(), appErr.Message())
			return
		}

		common.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	common.RespondJSON(w, http.StatusOK, todo)
}

func (h *TodosHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		if errors.Is(err, bson.ErrInvalidHex) {
			common.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		common.RespondError(w, http.StatusInternalServerError, err.Error())
	}

	claims, ok := middleware.GetAuthorization(r.Context())
	if !ok {
		err := middleware.ErrUnauthorized
		common.RespondError(w, err.Status(), err.Error())
		return
	}

	params, err := domain.ValidateUpdateTodoDTO(r)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			common.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		common.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	todo, err := h.svc.UpdateTodo(r.Context(), id, claims.ID, params)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			common.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		common.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	common.RespondJSON(w, http.StatusOK, todo)
}

func (h *TodosHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		if errors.Is(err, bson.ErrInvalidHex) {
			common.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		common.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	claims, ok := middleware.GetAuthorization(r.Context())
	if !ok {
		err := middleware.ErrUnauthorized
		common.RespondError(w, err.Status(), err.Error())
		return
	}

	if err := h.svc.DeleteTodo(r.Context(), id, claims.ID); err != nil {
		if appErr, ok := apperrors.From(err); ok {
			common.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		common.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	common.RespondJSON(w, http.StatusOK, map[string]any{})
}

func (h *TodosHandler) GettAll(w http.ResponseWriter, r *http.Request) {

	claims, ok := middleware.GetAuthorization(r.Context())
	if !ok {
		err := middleware.ErrUnauthorized
		common.RespondError(w, err.Status(), err.Error())
		return
	}

	todos, err := h.svc.GetTodos(r.Context(), claims.ID)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			common.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		common.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	common.RespondJSON(w, http.StatusOK, todos)
}
