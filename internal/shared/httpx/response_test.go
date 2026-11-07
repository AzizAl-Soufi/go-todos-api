package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
)

func TestRecovery_RepanicsErrAbortHandler(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	})

	defer func() {
		if r := recover(); r != http.ErrAbortHandler {
			t.Fatalf("recover() = %v, want http.ErrAbortHandler", r)
		}
	}()
	Recovery(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

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

func TestRespondErr(t *testing.T) {
	silenceLogs(t)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
	}{
		{"app error keeps status and message", apperrors.NotFound("NF", "todo not found"), http.StatusNotFound, "todo not found"},
		{"wrapped app error", fmt.Errorf("ctx: %w", apperrors.Validation("V", "bad title")), http.StatusBadRequest, "bad title"},
		{"plain error is hidden", errors.New("pq: connection refused at 10.0.0.5"), http.StatusInternalServerError, "internal server error"},
		{"internal app error is hidden", apperrors.InternalWrap("DB", "query failed on users", errors.New("secret detail")), http.StatusInternalServerError, "internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			RespondErr(rec, req, tt.err)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["error"] != tt.wantMessage {
				t.Fatalf("error = %q, want %q", body["error"], tt.wantMessage)
			}
		})
	}
}

func TestLimitBody(t *testing.T) {
	var readErr error
	h := LimitBody(8, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345678")))
	if readErr != nil {
		t.Fatalf("body at the limit: %v", readErr)
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("123456789")))
	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Fatalf("body over the limit: err = %v, want *http.MaxBytesError", readErr)
	}
}

func silenceLogs(t *testing.T) {
	t.Helper()
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })
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
	silenceLogs(t) // Recovery logs a stack trace

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	})

	rec := httptest.NewRecorder()
	Recovery(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if got, want := rec.Body.String(), "{\"error\":\"internal server error\"}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
