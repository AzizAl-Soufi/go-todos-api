// dto: Data Transfer Object used to represent request payloads separately
// from domain entities. They are used for validation and mapping input data.
package domain

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	apperrors "github.com/AzizAl-Soufi/todos-api/internal/common/errors"
)

type CreateTodoDTO struct {
	Title string `json:"title"`
}

var ErrInvalidRequestTitleValue apperrors.AppError = apperrors.Validation(
	"INVALID_TITLE",
	"invalid title value",
)

func ValidateCreateDTO(r *http.Request) (*CreateTodoDTO, error) {
	var dto CreateTodoDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		return nil, apperrors.ErrInvalidRequestBody
	}
	defer r.Body.Close()

	dto.Title = strings.TrimSpace(dto.Title)
	if dto.Title == "" {
		return nil, apperrors.Validation(
			"INVALID_TITLE",
			"invalid title value",
		)
	}

	return &dto, nil
}

type UpdateTodoDTO struct {
	Title     *string `json:"title,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
}

func ValidateUpdateTodoDTO(r *http.Request) (*UpdateTodoDTO, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, apperrors.ErrInvalidRequestBody
	}
	defer r.Body.Close()

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, apperrors.ErrInvalidRequestBody
	}

	if len(fields) == 0 {
		return nil, apperrors.ErrInvalidRequestBody
	}

	if value, ok := fields["title"]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, ErrInvalidRequestTitleValue
	}
	if value, ok := fields["completed"]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, apperrors.ErrInvalidRequestValue
	}

	var dto UpdateTodoDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		return nil, apperrors.ErrInvalidRequestBody
	}

	if dto.Title != nil {
		*dto.Title = strings.TrimSpace(*dto.Title)
		if *dto.Title == "" {
			return nil, ErrInvalidRequestTitleValue
		}
	}

	return &dto, nil
}

func (dto *UpdateTodoDTO) UpdateEntity(entity *Todo) (*Todo, error) {
	if dto.Title != nil {
		entity.Title = *dto.Title
	}

	if dto.Completed != nil {
		entity.Completed = *dto.Completed
	}

	return entity, nil
}
