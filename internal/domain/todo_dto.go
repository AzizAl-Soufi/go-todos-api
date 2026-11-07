// dto: Data Transfer Object used to represent request payloads separately
// from domain entities. They are used for validation and mapping input data.
package domain

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
)

type CreateTodoDTO struct {
	Title string `json:"title"`
}

var ErrInvalidRequestTitleValue apperrors.AppError = apperrors.Validation(
	"INVALID_TITLE",
	"invalid title value",
)

func ValidateCreateDTO(r *http.Request) (*CreateTodoDTO, error) {
	defer r.Body.Close()

	var dto CreateTodoDTO
	err := json.NewDecoder(r.Body).Decode(&dto)
	if err != nil {
		return nil, apperrors.ErrInvalidRequestBody
	}

	if dto.Title, err = normalizeTitle(dto.Title); err != nil {
		return nil, err
	}

	return &dto, nil
}

// normalizeTitle trims a title and rejects it if nothing is left.
func normalizeTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ErrInvalidRequestTitleValue
	}
	return title, nil
}

// isJSONNull reports whether a raw JSON value is the literal null.
func isJSONNull(v json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}

type UpdateTodoDTO struct {
	Title     *string `json:"title,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
}

func ValidateUpdateTodoDTO(r *http.Request) (*UpdateTodoDTO, error) {
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, apperrors.ErrInvalidRequestBody
	}

	// Decode into raw fields first: a pointer field can't tell "absent" from "null".
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || len(fields) == 0 {
		return nil, apperrors.ErrInvalidRequestBody
	}
	if v, ok := fields["title"]; ok && isJSONNull(v) {
		return nil, ErrInvalidRequestTitleValue
	}
	if v, ok := fields["completed"]; ok && isJSONNull(v) {
		return nil, apperrors.ErrInvalidRequestValue
	}

	var dto UpdateTodoDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		return nil, apperrors.ErrInvalidRequestBody
	}

	if dto.Title != nil {
		title, err := normalizeTitle(*dto.Title)
		if err != nil {
			return nil, err
		}
		dto.Title = &title
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
