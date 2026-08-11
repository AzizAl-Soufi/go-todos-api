package v1

import (
	"net/http"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/service"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/shared/httpx"
)

type UsersHandler struct {
	svc service.UsersService
}

func NewUsersHandler(svc service.UsersService) *UsersHandler {
	return &UsersHandler{svc: svc}
}

func (h *UsersHandler) Register(w http.ResponseWriter, r *http.Request) {
	dto, err := domain.ValidateUserDTO(r)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	authResponse, err := h.svc.Register(r.Context(), dto)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.RespondJSON(w, http.StatusCreated, authResponse)
}

func (h *UsersHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	dto, err := domain.ValidateRefreshRequestDTO(r)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	authResponse, err := h.svc.RefreshToken(r.Context(), dto)
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.RespondJSON(w, http.StatusCreated, authResponse)
}

func (h *UsersHandler) Auth(w http.ResponseWriter, r *http.Request) {
	overview, err := h.svc.Auth(r.Context())
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.RespondJSON(w, http.StatusCreated, overview)
}

func (h *UsersHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.svc.GetOverview(r.Context())
	if err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.RespondJSON(w, http.StatusOK, overview)
}

func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteAccount(r.Context()); err != nil {
		if appErr, ok := apperrors.From(err); ok {
			httpx.RespondError(w, appErr.Status(), appErr.Message())
			return
		}
		httpx.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.RespondJSON(w, http.StatusOK, map[string]any{})
}
