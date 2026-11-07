package domain

import (
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
)

func TestValidateCreateDTO_ReturnsParsedDTO(t *testing.T) {
	req := httptest.NewRequest("POST", "/todos", strings.NewReader(`{"title":"Buy milk"}`))

	dto, err := ValidateCreateDTO(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if dto == nil {
		t.Fatal("expected dto to be returned")
	}

	if dto.Title != "Buy milk" {
		t.Fatalf("expected title to be parsed, got %q", dto.Title)
	}
}

func TestValidateCreateDTO_TrimsTitle(t *testing.T) {
	req := httptest.NewRequest("POST", "/todos", strings.NewReader(`{"title":"  Buy milk \n"}`))

	dto, err := ValidateCreateDTO(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if dto.Title != "Buy milk" {
		t.Fatalf("expected trimmed title, got %q", dto.Title)
	}
}

func TestValidateCreateDTO_Errors(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		{"empty body", ``, "INVALID_BODY"},
		{"malformed json", `{"title":`, "INVALID_BODY"},
		{"wrong type", `{"title":123}`, "INVALID_BODY"},
		{"missing title", `{}`, "INVALID_TITLE"},
		{"blank title", `{"title":"   "}`, "INVALID_TITLE"},
		{"null title", `{"title":null}`, "INVALID_TITLE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/todos", strings.NewReader(tt.body))
			dto, err := ValidateCreateDTO(req)
			assertAppErrorCode(t, err, tt.wantCode)
			if dto != nil {
				t.Fatalf("expected nil dto, got %+v", dto)
			}
		})
	}
}

func TestValidateUpdateDTO_ReturnsParsedDTO(t *testing.T) {
	req := httptest.NewRequest("PUT", "/todos", strings.NewReader(`{"title":"Buy milk"}`))

	dto, err := ValidateUpdateTodoDTO(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if dto == nil {
		t.Fatal("expected dto to be returned")
	}

	if *dto.Title != "Buy milk" {
		t.Fatalf("expected title to be parsed, got %q", *dto.Title)
	}
}

func TestValidateUpdateDTO_ValidPayloads(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantTitle     *string
		wantCompleted *bool
	}{
		{"completed only", `{"completed":true}`, nil, ptr(true)},
		{"completed false", `{"completed":false}`, nil, ptr(false)},
		{"both fields, title trimmed", `{"title":"  x  ","completed":true}`, ptr("x"), ptr(true)},
		{"unknown field is ignored", `{"other":1}`, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("PATCH", "/todos/1", strings.NewReader(tt.body))
			dto, err := ValidateUpdateTodoDTO(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !equalPtr(dto.Title, tt.wantTitle) {
				t.Errorf("Title = %v, want %v", deref(dto.Title), deref(tt.wantTitle))
			}
			if !equalPtr(dto.Completed, tt.wantCompleted) {
				t.Errorf("Completed = %v, want %v", deref(dto.Completed), deref(tt.wantCompleted))
			}
		})
	}
}

func TestValidateUpdateDTO_Errors(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		{"empty body", ``, "INVALID_BODY"},
		{"json null", `null`, "INVALID_BODY"},
		{"json array", `[]`, "INVALID_BODY"},
		{"empty object", `{}`, "INVALID_BODY"},
		{"malformed json", `{"title":`, "INVALID_BODY"},
		{"null title", `{"title":null}`, "INVALID_TITLE"},
		{"null title with whitespace", `{"title": null }`, "INVALID_TITLE"},
		{"blank title", `{"title":"  "}`, "INVALID_TITLE"},
		{"null completed", `{"completed":null}`, "INVALID_VALUE"},
		{"wrong title type", `{"title":5}`, "INVALID_BODY"},
		{"wrong completed type", `{"completed":"yes"}`, "INVALID_BODY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("PATCH", "/todos/1", strings.NewReader(tt.body))
			dto, err := ValidateUpdateTodoDTO(req)
			assertAppErrorCode(t, err, tt.wantCode)
			if dto != nil {
				t.Fatalf("expected nil dto, got %+v", dto)
			}
		})
	}
}

func TestUpdateEntity(t *testing.T) {
	t.Run("applies set fields only", func(t *testing.T) {
		todo := &Todo{ID: "1", Title: "old", Completed: false}
		dto := &UpdateTodoDTO{Completed: ptr(true)}

		got, err := dto.UpdateEntity(todo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != todo {
			t.Fatal("expected the same entity to be returned")
		}
		if got.Title != "old" || !got.Completed {
			t.Fatalf("unexpected entity: %+v", got)
		}
	})

	t.Run("applies all fields", func(t *testing.T) {
		todo := &Todo{Title: "old", Completed: true}
		dto := &UpdateTodoDTO{Title: ptr("new"), Completed: ptr(false)}

		got, _ := dto.UpdateEntity(todo)
		if got.Title != "new" || got.Completed {
			t.Fatalf("unexpected entity: %+v", got)
		}
	})

	t.Run("empty dto is a no-op", func(t *testing.T) {
		todo := &Todo{Title: "same", Completed: true}
		got, _ := (&UpdateTodoDTO{}).UpdateEntity(todo)
		if got.Title != "same" || !got.Completed {
			t.Fatalf("unexpected entity: %+v", got)
		}
	})
}

func assertAppErrorCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", wantCode)
	}
	ae, ok := apperrors.From(err)
	if !ok {
		t.Fatalf("expected app error, got %T: %v", err, err)
	}
	if ae.Code() != wantCode {
		t.Fatalf("code = %q, want %q", ae.Code(), wantCode)
	}
}

func ptr[T any](v T) *T { return &v }

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
