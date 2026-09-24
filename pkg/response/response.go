package response

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/crm/backend/pkg/apperrors"
)

// Envelope is the standard API response wrapper.
type Envelope struct {
	Success   bool           `json:"success"`
	Data      any            `json:"data,omitempty"`
	Error     *ErrorBody     `json:"error,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// ErrorBody is the structured error payload.
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// JSON writes a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, payload Envelope) {
	payload.Timestamp = time.Now().UTC()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// OK writes a successful response.
func OK(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, Envelope{Success: true, Data: data})
}

// Created writes a 201 response.
func Created(w http.ResponseWriter, data any) {
	JSON(w, http.StatusCreated, Envelope{Success: true, Data: data})
}

// Fail writes an error response from an AppError or a generic internal error.
func Fail(w http.ResponseWriter, err error) {
	if appErr, ok := apperrors.AsAppError(err); ok {
		JSON(w, appErr.HTTPStatus, Envelope{
			Success: false,
			Error: &ErrorBody{
				Code:    string(appErr.Code),
				Message: appErr.Message,
				Details: appErr.Details,
			},
		})
		return
	}

	slog.Error("unhandled error", "error", err)
	JSON(w, http.StatusInternalServerError, Envelope{
		Success: false,
		Error: &ErrorBody{
			Code:    string(apperrors.CodeInternal),
			Message: "An unexpected error occurred",
		},
	})
}
