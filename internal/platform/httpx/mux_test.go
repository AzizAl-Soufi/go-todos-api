package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJSONErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		RespondJSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id")})
	})
	h := JSONErrors(mux)

	tests := []struct {
		method, path string
		status       int
		body, allow  string
	}{
		{"GET", "/items/7", 200, `{"id":"7"}` + "\n", ""},
		{"GET", "/nope", 404, `{"error":{"code":"NOT_FOUND","message":"route not found"}}` + "\n", ""},
		{"POST", "/items/7", 405, `{"error":{"code":"METHOD_NOT_ALLOWED","message":"method not allowed"}}` + "\n", "GET, HEAD"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.status || rec.Body.String() != tt.body {
				t.Fatalf("got %d %q, want %d %q", rec.Code, rec.Body.String(), tt.status, tt.body)
			}
			if got := rec.Header().Get("Allow"); got != tt.allow {
				t.Fatalf("Allow = %q, want %q", got, tt.allow)
			}
		})
	}
}
