package httpx

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRespondJSON(t *testing.T) {
	rec := httptest.NewRecorder()

	RespondJSON(rec, http.StatusCreated, map[string]any{"id": "1", "ok": true})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["id"] != "1" || body["ok"] != true {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestRespondJSON_NilPayload(t *testing.T) {
	rec := httptest.NewRecorder()

	RespondJSON(rec, http.StatusOK, nil)

	if got := rec.Body.String(); got != "null\n" {
		t.Fatalf("body = %q, want %q", got, "null\n")
	}
}

func TestRespondError(t *testing.T) {
	rec := httptest.NewRecorder()

	RespondError(rec, http.StatusNotFound, "entity not found")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "entity not found" {
		t.Fatalf("error = %q, want %q", body["error"], "entity not found")
	}
}

func TestRecovery_PassesThroughWithoutPanic(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	rec := httptest.NewRecorder()
	Recovery(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}

func TestRecovery_ConvertsPanicTo500(t *testing.T) {
	// Silence the stack trace Recovery logs.
	orig := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(orig) })

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	})

	rec := httptest.NewRecorder()
	Recovery(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got, want := rec.Body.String(), "{\"error\":\"internal server error\"}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
