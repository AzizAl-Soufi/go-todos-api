package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
)

// DefaultMaxBodyBytes caps request bodies accepted by LimitBody.
const DefaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

const internalErrorMessage = "internal server error"

func RespondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func RespondError(w http.ResponseWriter, status int, message string) {
	RespondJSON(w, status, map[string]string{"error": message})
}

// RespondErr writes err as a JSON error response. Application errors keep
// their status and message; anything else is logged and reported as a generic
// 500 so internal details (driver errors, SQL, etc.) never reach the client.
func RespondErr(w http.ResponseWriter, r *http.Request, err error) {
	if appErr, ok := apperrors.From(err); ok {
		if appErr.Status() >= http.StatusInternalServerError {
			slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
			RespondError(w, appErr.Status(), internalErrorMessage)
			return
		}
		RespondError(w, appErr.Status(), appErr.Message())
		return
	}

	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	RespondError(w, http.StatusInternalServerError, internalErrorMessage)
}

// LimitBody rejects request bodies larger than maxBytes. Reads past the limit
// fail, which the DTO validators report as an invalid request body.
func LimitBody(maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		next.ServeHTTP(w, r)
	})
}

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec) // let net/http abort the connection as intended
				}
				slog.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				RespondError(w, http.StatusInternalServerError, internalErrorMessage)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
