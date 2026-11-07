package v1

import (
	"net/http"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/service"
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
		httpx.RespondErr(w, r, err)
		return
	}

	authResponse, err := h.svc.Register(r.Context(), dto)
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	httpx.RespondJSON(w, http.StatusCreated, authResponse)
}

func (h *UsersHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	dto, err := domain.ValidateRefreshRequestDTO(r)
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	tokens, err := h.svc.RefreshToken(r.Context(), dto)
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	httpx.RespondJSON(w, http.StatusCreated, tokens)
}

func (h *UsersHandler) Auth(w http.ResponseWriter, r *http.Request) {
	overview, err := h.svc.Auth(r.Context())
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	httpx.RespondJSON(w, http.StatusCreated, overview)
}

func (h *UsersHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.svc.GetOverview(r.Context())
	if err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	httpx.RespondJSON(w, http.StatusOK, overview)
}

func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteAccount(r.Context()); err != nil {
		httpx.RespondErr(w, r, err)
		return
	}

	httpx.RespondJSON(w, http.StatusOK, map[string]any{})
}
