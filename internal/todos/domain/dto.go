// dto: Data Transfer Object used to represent request payloads separately
// from domain entities. They are used for validation and mapping input data.
package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

var (
	InvalidRequestTitleValue = errors.New("invalid title value")
	InvalidRequestBody       = errors.New("invalid request body")
	InvalidRequestValue      = errors.New("invalid request value")
)

type CreateTodoDTO struct {
	Title string `json:"title"`
}

func ValidateCreateDTO(r *http.Request) (*CreateTodoDTO, error) {
	var dto CreateTodoDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		return nil, InvalidRequestBody
	}
	defer r.Body.Close()

	dto.Title = strings.TrimSpace(dto.Title)
	if dto.Title == "" {
		return nil, InvalidRequestTitleValue
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
		return nil, InvalidRequestBody
	}
	defer r.Body.Close()

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, InvalidRequestBody
	}

	if len(fields) == 0 {
		return nil, InvalidRequestBody
	}

	if value, ok := fields["title"]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, InvalidRequestTitleValue
	}
	if value, ok := fields["completed"]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, InvalidRequestValue
	}

	var dto UpdateTodoDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		return nil, InvalidRequestBody
	}

	if dto.Title != nil {
		*dto.Title = strings.TrimSpace(*dto.Title)
		if *dto.Title == "" {
			return nil, InvalidRequestTitleValue
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
