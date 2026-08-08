package handler

import (
	"errors"
	"net/http"

	common "github.com/AzizAl-Soufi/todos-api/internal/common"
	"github.com/AzizAl-Soufi/todos-api/internal/users/domain"
	"github.com/AzizAl-Soufi/todos-api/internal/users/service"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type UsersHandler struct {
	svc service.UsersService
}

func NewUsersHandler(svc service.UsersService) *UsersHandler {
	return &UsersHandler{svc: svc}
}

func (h *UsersHandler) Auth(w http.ResponseWriter, r *http.Request) {
	dto, err := domain.ValidateUserDTO(r)
	if err != nil {
		common.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	overview, err := h.svc.Auth(r.Context(), dto)
	if err != nil {
		common.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	common.RespondJSON(w, http.StatusCreated, overview)
}

func (h *UsersHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("userId")
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		if errors.Is(err, bson.ErrInvalidHex) {
			common.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	overview, err := h.svc.GetOverview(r.Context(), id)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			common.RespondError(w, http.StatusNotFound, err.Error())
			return
		}
		common.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	common.RespondJSON(w, http.StatusOK, overview)
}

func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("userId")
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		if errors.Is(err, bson.ErrInvalidHex) {
			common.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		common.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := h.svc.DeleteAccount(r.Context(), id); err != nil {
		if errors.Is(err, common.ErrNotFound) {
			common.RespondError(w, http.StatusNotFound, err.Error())
		}
		return
	}

	common.RespondJSON(w, http.StatusOK, map[string]any{})
}
