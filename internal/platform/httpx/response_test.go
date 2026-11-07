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
	"time"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
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

	RespondError(rec, http.StatusNotFound, "NOT_FOUND", "entity not found")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	if got, want := rec.Body.String(), `{"error":{"code":"NOT_FOUND","message":"entity not found"}}`+"\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestRespondErr(t *testing.T) {
	silenceLogs(t)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{"app error keeps status and message", apperrors.NotFound("NF", "todo not found"), http.StatusNotFound, "NF", "todo not found"},
		{"wrapped app error", fmt.Errorf("ctx: %w", apperrors.Validation("V", "bad title")), http.StatusBadRequest, "V", "bad title"},
		{"plain error is hidden", errors.New("pq: connection refused at 10.0.0.5"), http.StatusInternalServerError, "INTERNAL", "internal server error"},
		{"internal app error is hidden", apperrors.InternalWrap("DB", "query failed on users", errors.New("secret detail")), http.StatusInternalServerError, "INTERNAL", "internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			RespondErr(rec, req, tt.err)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body errorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != tt.wantCode || body.Error.Message != tt.wantMessage {
				t.Fatalf("error = %+v, want code %q message %q", body.Error, tt.wantCode, tt.wantMessage)
			}
			if body.Error.RetryAt != nil || rec.Header().Get("Retry-After") != "" {
				t.Fatalf("retry hints on an error that has none: %s", rec.Body)
			}
		})
	}
}

func TestRespondErr_RetryLater(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.FixedZone("CET", 3600))
	at := now.Add(90*time.Second + 300*time.Millisecond)
	err := fmt.Errorf("resend: %w", apperrors.RetryAt(apperrors.TooManyRequests("SOON", "try later"), now, at))

	rec := httptest.NewRecorder()
	RespondErr(rec, httptest.NewRequest(http.MethodPost, "/x", nil), err)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", rec.Code)
	}
	// Rounded up to the second, in UTC; Retry-After counts from the caller's clock.
	if got := rec.Header().Get("Retry-After"); got != "91" {
		t.Fatalf("Retry-After = %q, want 91", got)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			RetryAt string `json:"retryAt"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "SOON" || body.Error.RetryAt != "2026-11-07T11:01:31Z" {
		t.Fatalf("body = %s, want code SOON and retryAt 2026-11-07T11:01:31Z", rec.Body)
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
	if got, want := rec.Body.String(), `{"error":{"code":"INTERNAL","message":"internal server error"}}`+"\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
