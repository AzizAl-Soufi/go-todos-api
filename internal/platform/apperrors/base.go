package apperrors

import (
	"errors"
	"fmt"
	"net/http"
)

type baseError struct {
	status  int
	code    string
	message string
	err     error
}

func (e *baseError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.code, e.message, e.err)
	}
	return fmt.Sprintf("[%s] %s", e.code, e.message)
}

func (e *baseError) Status() int     { return e.status }
func (e *baseError) Code() string    { return e.code }
func (e *baseError) Message() string { return e.message }
func (e *baseError) Unwrap() error   { return e.err }

type (
	ValidationError      struct{ baseError }
	NotFoundError        struct{ baseError }
	ConflictError        struct{ baseError }
	UnauthorizedError    struct{ baseError }
	ForbiddenError       struct{ baseError }
	TooManyRequestsError struct{ baseError }
	InternalError        struct{ baseError }
)

func base(status int, code, message string) baseError {
	return baseError{status: status, code: code, message: message}
}

func Validation(code, message string) *ValidationError {
	return &ValidationError{base(http.StatusBadRequest, code, message)}
}

func Validationf(code, format string, args ...any) *ValidationError {
	return Validation(code, fmt.Sprintf(format, args...))
}

func NotFound(code, message string) *NotFoundError {
	return &NotFoundError{base(http.StatusNotFound, code, message)}
}

func NotFoundf(code, format string, args ...any) *NotFoundError {
	return NotFound(code, fmt.Sprintf(format, args...))
}

func Conflict(code, message string) *ConflictError {
	return &ConflictError{base(http.StatusConflict, code, message)}
}

func Conflictf(code, format string, args ...any) *ConflictError {
	return Conflict(code, fmt.Sprintf(format, args...))
}

func Unauthorized(code, message string) *UnauthorizedError {
	return &UnauthorizedError{base(http.StatusUnauthorized, code, message)}
}

func Unauthorizedf(code, format string, args ...any) *UnauthorizedError {
	return Unauthorized(code, fmt.Sprintf(format, args...))
}

func Forbidden(code, message string) *ForbiddenError {
	return &ForbiddenError{base(http.StatusForbidden, code, message)}
}

func TooManyRequests(code, message string) *TooManyRequestsError {
	return &TooManyRequestsError{base(http.StatusTooManyRequests, code, message)}
}

func Internal(code, message string) *InternalError {
	return &InternalError{base(http.StatusInternalServerError, code, message)}
}

func InternalWrap(code, message string, err error) *InternalError {
	e := Internal(code, message)
	e.err = err
	return e
}

func From(err error) (AppError, bool) {
	var ae AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// NotFoundAs replaces ErrNotFound (from a repository) with a more specific
// error, such as a domain "project not found" or "invalid token". Any other
// error, and nil, is returned unchanged.
func NotFoundAs(err, replacement error) error {
	if errors.Is(err, ErrNotFound) {
		return replacement
	}
	return err
}

// UnresolvedAs is NotFoundAs for an ID the client referenced (in a request
// body or a filter): a malformed ID can't name an existing record either, so
// ErrInvalidID is replaced too.
func UnresolvedAs(err, replacement error) error {
	if errors.Is(err, ErrInvalidID) {
		return replacement
	}
	return NotFoundAs(err, replacement)
}
