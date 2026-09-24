package apperrors

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a stable machine-readable error code.
type Code string

const (
	CodeValidation   Code = "validation_error"
	CodeUnauthorized Code = "unauthorized"
	CodeForbidden    Code = "forbidden"
	CodeNotFound     Code = "not_found"
	CodeConflict     Code = "conflict"
	CodeInternal     Code = "internal_error"
)

// AppError is a typed application error with HTTP status mapping.
type AppError struct {
	Code       Code
	Message    string
	HTTPStatus int
	Err        error
	Details    map[string]any
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(code Code, message string, httpStatus int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
	}
}

func Wrap(err error, code Code, message string, httpStatus int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
		Err:        err,
	}
}

func Validation(message string) *AppError {
	return New(CodeValidation, message, http.StatusBadRequest)
}

func ValidationDetails(message string, details map[string]any) *AppError {
	err := Validation(message)
	err.Details = details
	return err
}

// FieldValidation returns a validation error with field + message details.
func FieldValidation(field, message string) *AppError {
	return ValidationDetails(message, map[string]any{
		"field":   field,
		"message": message,
	})
}

func Unauthorized(message string) *AppError {
	return New(CodeUnauthorized, message, http.StatusUnauthorized)
}

func Forbidden(message string) *AppError {
	return New(CodeForbidden, message, http.StatusForbidden)
}

func NotFound(message string) *AppError {
	return New(CodeNotFound, message, http.StatusNotFound)
}

func Conflict(message string) *AppError {
	return New(CodeConflict, message, http.StatusConflict)
}

func Internal(message string, err error) *AppError {
	return Wrap(err, CodeInternal, message, http.StatusInternalServerError)
}

// AsAppError extracts an AppError from an error chain.
func AsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}
