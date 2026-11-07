package apperrors

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestConstructors_SetStatusCodeAndMessage(t *testing.T) {
	tests := []struct {
		name    string
		err     AppError
		status  int
		code    string
		message string
	}{
		{"Validation", Validation("V", "bad"), http.StatusBadRequest, "V", "bad"},
		{"Validationf", Validationf("V", "bad %s %d", "x", 1), http.StatusBadRequest, "V", "bad x 1"},
		{"NotFound", NotFound("NF", "missing"), http.StatusNotFound, "NF", "missing"},
		{"NotFoundf", NotFoundf("NF", "missing %q", "id"), http.StatusNotFound, "NF", `missing "id"`},
		{"Conflict", Conflict("C", "dup"), http.StatusConflict, "C", "dup"},
		{"Conflictf", Conflictf("C", "dup %d", 2), http.StatusConflict, "C", "dup 2"},
		{"Unauthorized", Unauthorized("U", "nope"), http.StatusUnauthorized, "U", "nope"},
		{"Unauthorizedf", Unauthorizedf("U", "nope %v", true), http.StatusUnauthorized, "U", "nope true"},
		{"Forbidden", Forbidden("F", "denied"), http.StatusForbidden, "F", "denied"},
		{"Internal", Internal("I", "boom"), http.StatusInternalServerError, "I", "boom"},
		{"TooManyRequests", TooManyRequests("R", "slow down"), http.StatusTooManyRequests, "R", "slow down"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Status(); got != tt.status {
				t.Errorf("Status() = %d, want %d", got, tt.status)
			}
			if got := tt.err.Code(); got != tt.code {
				t.Errorf("Code() = %q, want %q", got, tt.code)
			}
			if got := tt.err.Message(); got != tt.message {
				t.Errorf("Message() = %q, want %q", got, tt.message)
			}
			if tt.err.Unwrap() != nil {
				t.Errorf("Unwrap() = %v, want nil", tt.err.Unwrap())
			}
			want := fmt.Sprintf("[%s] %s", tt.code, tt.message)
			if got := tt.err.Error(); got != want {
				t.Errorf("Error() = %q, want %q", got, want)
			}
		})
	}
}

func TestInternalWrap_IncludesAndUnwrapsCause(t *testing.T) {
	cause := errors.New("db down")
	err := InternalWrap("DB", "query failed", cause)

	if err.Status() != http.StatusInternalServerError {
		t.Fatalf("Status() = %d, want 500", err.Status())
	}
	if got, want := err.Error(), "[DB] query failed: db down"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("expected errors.Is to find the wrapped cause")
	}
}

func TestFrom(t *testing.T) {
	t.Run("direct app error", func(t *testing.T) {
		ae, ok := From(ErrNotFound)
		if !ok || ae.Status() != http.StatusNotFound {
			t.Fatalf("From(ErrNotFound) = %v, %v", ae, ok)
		}
	})

	t.Run("wrapped app error", func(t *testing.T) {
		wrapped := fmt.Errorf("context: %w", ErrDuplicate)
		ae, ok := From(wrapped)
		if !ok || ae.Code() != "DUPLICATE_ENTITY" {
			t.Fatalf("From(wrapped) = %v, %v", ae, ok)
		}
	})

	t.Run("plain error", func(t *testing.T) {
		ae, ok := From(errors.New("plain"))
		if ok || ae != nil {
			t.Fatalf("From(plain) = %v, %v; want nil, false", ae, ok)
		}
	})

	t.Run("nil", func(t *testing.T) {
		if _, ok := From(nil); ok {
			t.Fatal("From(nil) should not report an app error")
		}
	})
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		err    AppError
		status int
		code   string
	}{
		{ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{ErrDuplicate, http.StatusConflict, "DUPLICATE_ENTITY"},
		{ErrExistingUser, http.StatusConflict, "USER_EXISTS"},
		{ErrInvalidRequestBody, http.StatusBadRequest, "INVALID_BODY"},
		{ErrInvalidRequestValue, http.StatusBadRequest, "INVALID_VALUE"},
		{ErrInvalidEntity, http.StatusBadRequest, "INVALID_ENTITY"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if tt.err.Status() != tt.status || tt.err.Code() != tt.code {
				t.Fatalf("got (%d, %q), want (%d, %q)", tt.err.Status(), tt.err.Code(), tt.status, tt.code)
			}
		})
	}
}

func TestTypedErrorsAreDistinguishable(t *testing.T) {
	var err error = NotFound("X", "y")

	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatal("expected errors.As to match *NotFoundError")
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		t.Fatal("did not expect errors.As to match *ValidationError")
	}
}

func TestNotFoundAs(t *testing.T) {
	specific := NotFound("PROJECT_NOT_FOUND", "project not found")
	other := errors.New("db down")
	for name, tc := range map[string]struct{ in, want error }{
		"not found":         {fmt.Errorf("get: %w", ErrNotFound), specific},
		"other error":       {other, other},
		"nil":               {nil, nil},
		"invalid id passes": {ErrInvalidID, ErrInvalidID},
	} {
		if got := NotFoundAs(tc.in, specific); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

func TestUnresolvedAs(t *testing.T) {
	specific := NotFound("PROJECT_NOT_FOUND", "project not found")
	for _, in := range []error{ErrNotFound, ErrInvalidID} {
		if got := UnresolvedAs(in, specific); got != specific {
			t.Errorf("UnresolvedAs(%v) = %v", in, got)
		}
	}
	if other := errors.New("db down"); UnresolvedAs(other, specific) != other {
		t.Error("other errors pass through")
	}
}
