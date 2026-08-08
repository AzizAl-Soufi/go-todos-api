package domain

import (
	"net/http/httptest"
	"strings"
	"testing"
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
