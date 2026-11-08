package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

func TestEndpoint(t *testing.T) {
	silenceLogs(t)
	for name, tc := range map[string]struct {
		e      Endpoint
		status int
		body   string
	}{
		"json":       {func(*http.Request) (int, any, error) { return 201, map[string]int{"n": 1}, nil }, 201, `{"n":1}` + "\n"},
		"no content": {func(*http.Request) (int, any, error) { return 204, "ignored", nil }, 204, ""},
		"app error":  {func(*http.Request) (int, any, error) { return 0, nil, apperrors.ErrNotFound }, 404, `{"error":{"code":"NOT_FOUND","message":"entity not found"}}` + "\n"},
		"hidden":     {func(*http.Request) (int, any, error) { return 0, nil, errors.New("secret") }, 500, `{"error":{"code":"INTERNAL","message":"internal server error"}}` + "\n"},
	} {
		rec := httptest.NewRecorder()
		tc.e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != tc.status || rec.Body.String() != tc.body {
			t.Errorf("%s: got %d %q, want %d %q", name, rec.Code, rec.Body.String(), tc.status, tc.body)
		}
	}
}
