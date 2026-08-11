package domain

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/middleware"
)

type UserDTO struct {
	Name  string `json:"name" bson:"name"`
	Email string `json:"email" bson:"email"`
}

func ValidateUserDTO(r *http.Request) (*UserDTO, error) {
	var dto UserDTO
	err := json.NewDecoder(r.Body).Decode(&dto)

	if err != nil {
		return nil, apperrors.ErrInvalidRequestBody
	}

	if strings.TrimSpace(dto.Name) == "" {
		return nil, apperrors.Validation("INVALID_NAME", "name is required")
	}

	if strings.TrimSpace(dto.Email) == "" {
		return nil, apperrors.Validation("INVALID_EMAIL", "email is required")
	}

	address, err := mail.ParseAddress(dto.Email)
	if err != nil || address.Address != dto.Email {
		return nil, apperrors.Validation("INVALID_EMAIL", "invalid email")
	}

	return &dto, nil
}

type RegisterUserResponse struct {
	Overview      *Overview             `json:"overview"`
	Authorization *middleware.TokenPair `json:"authorization"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func ValidateRefreshRequestDTO(r *http.Request) (*RefreshRequest, error) {
	var dto RefreshRequest
	_ = json.NewDecoder(r.Body).Decode(&dto)

	if strings.TrimSpace(dto.RefreshToken) == "" {
		return nil, apperrors.Validation("INVALID_VALUE", "Refresh token is required")
	}

	return &dto, nil
}
