package domain

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateUserDTO_Valid(t *testing.T) {
	req := httptest.NewRequest("POST", "/users", strings.NewReader(`{"name":"Alice","email":"alice@example.com"}`))

	dto, err := ValidateUserDTO(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Name != "Alice" || dto.Email != "alice@example.com" {
		t.Fatalf("unexpected dto: %+v", dto)
	}
}

func TestValidateUserDTO_Errors(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		{"empty body", ``, "INVALID_BODY"},
		{"malformed json", `{"name":`, "INVALID_BODY"},
		{"missing name", `{"email":"a@b.com"}`, "INVALID_NAME"},
		{"blank name", `{"name":"  ","email":"a@b.com"}`, "INVALID_NAME"},
		{"missing email", `{"name":"Alice"}`, "INVALID_EMAIL"},
		{"blank email", `{"name":"Alice","email":"   "}`, "INVALID_EMAIL"},
		{"not an email", `{"name":"Alice","email":"alice"}`, "INVALID_EMAIL"},
		{"display-name form", `{"name":"Alice","email":"Alice <a@b.com>"}`, "INVALID_EMAIL"},
		{"surrounding whitespace", `{"name":"Alice","email":" a@b.com "}`, "INVALID_EMAIL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/users", strings.NewReader(tt.body))
			dto, err := ValidateUserDTO(req)
			assertAppErrorCode(t, err, tt.wantCode)
			if dto != nil {
				t.Fatalf("expected nil dto, got %+v", dto)
			}
		})
	}
}

func TestValidateRefreshRequestDTO(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/refresh", strings.NewReader(`{"refresh_token":"abc"}`))
		dto, err := ValidateRefreshRequestDTO(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dto.RefreshToken != "abc" {
			t.Fatalf("RefreshToken = %q, want abc", dto.RefreshToken)
		}
	})

	for _, tt := range []struct{ name, body string }{
		{"empty body", ``},
		{"malformed json", `{`},
		{"missing token", `{}`},
		{"blank token", `{"refresh_token":"  "}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/refresh", strings.NewReader(tt.body))
			dto, err := ValidateRefreshRequestDTO(req)
			assertAppErrorCode(t, err, "INVALID_VALUE")
			if dto != nil {
				t.Fatalf("expected nil dto, got %+v", dto)
			}
		})
	}
}
