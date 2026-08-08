package domain

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
)

type UserDTO struct {
	Name  string `json:"name" bson:"name"`
	Email string `json:"email" bson:"email"`
}

func ValidateUserDTO(r *http.Request) (*UserDTO, error) {
	var dto UserDTO
	_ = json.NewDecoder(r.Body).Decode(&dto)

	if strings.TrimSpace(dto.Name) == "" {
		return nil, errors.New("name is required")
	}

	if strings.TrimSpace(dto.Email) == "" {
		return nil, errors.New("email is required")
	}

	address, err := mail.ParseAddress(dto.Email)
	if err != nil || address.Address != dto.Email {
		return nil, errors.New("invalid email")
	}

	return &dto, nil
}
