package validate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/crm/backend/pkg/apperrors"
)

// DecodeJSON decodes a JSON request body into dest.
func DecodeJSON(r *http.Request, dest any) error {
	if r.Body == nil {
		return apperrors.Validation("request body is required")
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dest); err != nil {
		return apperrors.Validation(fmt.Sprintf("invalid JSON body: %v", err))
	}

	return nil
}

// RequiredString checks that a string field is non-empty after trimming.
func RequiredString(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return apperrors.Validation(fmt.Sprintf("%s is required", field))
	}
	return nil
}
