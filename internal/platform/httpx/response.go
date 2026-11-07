package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// DefaultMaxBodyBytes caps request bodies accepted by LimitBody.
const DefaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

const (
	internalErrorCode    = "INTERNAL"
	internalErrorMessage = "internal server error"
)

func RespondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// RespondError writes the standard error body:
//
//	{"error": {"code": "<CODE>", "message": "<human-readable explanation>"}}
//
// "code" is stable and meant for programmatic handling; "message" may change.
// Errors the client may retry later (apperrors.RetryLaterError) also carry
// "retryAt", the UTC time from which to retry, and a Retry-After header.
func RespondError(w http.ResponseWriter, status int, code, message string) {
	RespondJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// RespondErr writes err as a JSON error response. Application errors keep
// their status and message; anything else is logged and reported as a generic
// 500 so internal details (driver errors, SQL, etc.) never reach the client.
// The body has the RespondError shape.
func RespondErr(w http.ResponseWriter, r *http.Request, err error) {
	appErr, ok := apperrors.From(err)
	if !ok || appErr.Status() >= http.StatusInternalServerError {
		slog.Error("request failed",
			"method", r.Method, "path", r.URL.Path,
			"request_id", RequestIDFrom(r.Context()), "error", err)
		RespondError(w, http.StatusInternalServerError, internalErrorCode, internalErrorMessage)
		return
	}
	detail := errorDetail{Code: appErr.Code(), Message: appErr.Message()}
	var later apperrors.RetryLaterError
	if errors.As(err, &later) {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(later.Wait.Seconds()))))
		detail.RetryAt = &later.At
	}
	RespondJSON(w, appErr.Status(), errorBody{Error: detail})
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string     `json:"code"`
	Message string     `json:"message"`
	RetryAt *time.Time `json:"retryAt,omitempty"` // UTC
}

// RespondNoContent writes 204 No Content.
func RespondNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// DecodeJSON decodes the request body into v. Any decoding problem (malformed
// JSON, wrong types, body too large, trailing data) is reported as
// apperrors.ErrInvalidRequestBody.
func DecodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return apperrors.ErrInvalidRequestBody
	}
	if dec.More() {
		return apperrors.ErrInvalidRequestBody
	}
	return nil
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
				RespondError(w, http.StatusInternalServerError, internalErrorCode, internalErrorMessage)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
