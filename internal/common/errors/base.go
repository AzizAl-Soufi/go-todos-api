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
	ValidationError   struct{ baseError }
	NotFoundError     struct{ baseError }
	ConflictError     struct{ baseError }
	UnauthorizedError struct{ baseError }
	ForbiddenError    struct{ baseError }
	InternalError     struct{ baseError }
)

func Validation(code, message string) *ValidationError {
	return &ValidationError{baseError{status: http.StatusBadRequest, code: code, message: message}}
}
func Validationf(code, format string, args ...any) *ValidationError {
	return &ValidationError{baseError{status: http.StatusBadRequest, code: code, message: fmt.Sprintf(format, args...)}}
}

func NotFound(code, message string) *NotFoundError {
	return &NotFoundError{baseError{status: http.StatusNotFound, code: code, message: message}}
}

func NotFoundf(code, format string, args ...any) *NotFoundError {
	return &NotFoundError{baseError{status: http.StatusNotFound, code: code, message: fmt.Sprintf(format, args...)}}
}

func Conflict(code, message string) *ConflictError {
	return &ConflictError{baseError{status: http.StatusConflict, code: code, message: message}}
}
func Conflictf(code, format string, args ...any) *ConflictError {
	return &ConflictError{baseError{status: http.StatusConflict, code: code, message: fmt.Sprintf(format, args...)}}
}

func Unauthorized(code, message string) *UnauthorizedError {
	return &UnauthorizedError{baseError{status: http.StatusUnauthorized, code: code, message: message}}
}

func Unauthorizedf(code, format string, args ...any) *UnauthorizedError {
	return &UnauthorizedError{baseError{status: http.StatusUnauthorized, code: code, message: fmt.Sprintf(format, args...)}}
}

func Forbidden(code, message string) *ForbiddenError {
	return &ForbiddenError{baseError{status: http.StatusForbidden, code: code, message: message}}
}

func Internal(code, message string) *InternalError {
	return &InternalError{baseError{status: http.StatusInternalServerError, code: code, message: message}}
}
func InternalWrap(code, message string, err error) *InternalError {
	return &InternalError{baseError{status: http.StatusInternalServerError, code: code, message: message, err: err}}
}

func From(err error) (AppError, bool) {
	var ae AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}
