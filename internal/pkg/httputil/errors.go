// Package httputil provides HTTP utilities including consistent error responses.
package httputil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/homeport/homeport/internal/pkg/logger"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// ErrorResponse represents a consistent error response format.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

// Error codes for consistent error identification.
const (
	CodeBadRequest          = "BAD_REQUEST"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeForbidden           = "FORBIDDEN"
	CodeNotFound            = "NOT_FOUND"
	CodeInternalError       = "INTERNAL_ERROR"
	CodeServiceUnavailable  = "SERVICE_UNAVAILABLE"
	CodeBadGateway          = "BAD_GATEWAY"
	CodeValidationFailed    = "VALIDATION_FAILED"
	CodeInvalidJSON         = "INVALID_JSON"
	CodeRequestTooLarge     = "REQUEST_TOO_LARGE"
	CodeTooManyRequests     = "TOO_MANY_REQUESTS"
	CodeAccountLocked       = "ACCOUNT_LOCKED"
)

// WriteError writes a consistent JSON error response.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details string) {
	// Sanitize details to mask sensitive data before logging
	sanitizedDetails := SanitizeString(details)

	// Log the error
	reqID := chimiddleware.GetReqID(r.Context())
	logMsg := "HTTP error"
	if reqID != "" {
		logger.Error(logMsg,
			"request_id", reqID,
			"status", status,
			"code", code,
			"message", message,
			"details", sanitizedDetails,
			"path", r.URL.Path,
			"method", r.Method,
		)
	} else {
		logger.Error(logMsg,
			"status", status,
			"code", code,
			"message", message,
			"details", sanitizedDetails,
			"path", r.URL.Path,
			"method", r.Method,
		)
	}

	resp := ErrorResponse{
		Error: message,
		Code:  code,
	}
	if sanitizedDetails != "" {
		resp.Details = sanitizedDetails
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// BadRequest writes a 400 Bad Request error response.
func BadRequest(w http.ResponseWriter, r *http.Request, message string) {
	WriteError(w, r, http.StatusBadRequest, CodeBadRequest, message, "")
}

// BadRequestWithDetails writes a 400 Bad Request error response with details.
func BadRequestWithDetails(w http.ResponseWriter, r *http.Request, message, details string) {
	WriteError(w, r, http.StatusBadRequest, CodeBadRequest, message, details)
}

// Unauthorized writes a 401 Unauthorized error response.
func Unauthorized(w http.ResponseWriter, r *http.Request, message string) {
	if message == "" {
		message = "Unauthorized"
	}
	WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, message, "")
}

// Forbidden writes a 403 Forbidden error response.
func Forbidden(w http.ResponseWriter, r *http.Request, message string) {
	if message == "" {
		message = "Forbidden"
	}
	WriteError(w, r, http.StatusForbidden, CodeForbidden, message, "")
}

// NotFound writes a 404 Not Found error response.
func NotFound(w http.ResponseWriter, r *http.Request, message string) {
	if message == "" {
		message = "Not found"
	}
	WriteError(w, r, http.StatusNotFound, CodeNotFound, message, "")
}

// InternalError writes a 500 Internal Server Error response.
func InternalError(w http.ResponseWriter, r *http.Request, err error) {
	message := "Internal server error"
	details := ""
	if err != nil {
		details = err.Error()
	}
	WriteError(w, r, http.StatusInternalServerError, CodeInternalError, message, details)
}

// InternalErrorWithMessage writes a 500 Internal Server Error response with a custom message.
func InternalErrorWithMessage(w http.ResponseWriter, r *http.Request, message string, err error) {
	details := ""
	if err != nil {
		details = err.Error()
	}
	WriteError(w, r, http.StatusInternalServerError, CodeInternalError, message, details)
}

// ServiceUnavailable writes a 503 Service Unavailable error response.
func ServiceUnavailable(w http.ResponseWriter, r *http.Request, message string) {
	if message == "" {
		message = "Service unavailable"
	}
	WriteError(w, r, http.StatusServiceUnavailable, CodeServiceUnavailable, message, "")
}

// BadGateway writes a 502 Bad Gateway error response.
func BadGateway(w http.ResponseWriter, r *http.Request, message string, err error) {
	details := ""
	if err != nil {
		details = err.Error()
	}
	WriteError(w, r, http.StatusBadGateway, CodeBadGateway, message, details)
}

// InvalidJSON writes a 400 error for JSON parsing errors with helpful details.
func InvalidJSON(w http.ResponseWriter, r *http.Request, err error) {
	message := "Invalid JSON in request body"
	details := ""

	if err != nil {
		var syntaxErr *json.SyntaxError
		var unmarshalErr *json.UnmarshalTypeError

		switch {
		case errors.As(err, &syntaxErr):
			details = "Syntax error at position " + string(rune(syntaxErr.Offset))
		case errors.As(err, &unmarshalErr):
			details = "Field '" + unmarshalErr.Field + "' has wrong type, expected " + unmarshalErr.Type.String()
		case errors.Is(err, io.EOF):
			details = "Request body is empty"
		case strings.Contains(err.Error(), "unexpected end of JSON"):
			details = "Incomplete JSON body"
		default:
			details = err.Error()
		}
	}

	WriteError(w, r, http.StatusBadRequest, CodeInvalidJSON, message, details)
}

// RequestTooLarge writes a 413 Request Entity Too Large error response.
func RequestTooLarge(w http.ResponseWriter, r *http.Request, maxSize int64) {
	message := "Request body too large"
	details := ""
	if maxSize > 0 {
		details = "Maximum allowed size: " + formatBytes(maxSize)
	}
	WriteError(w, r, http.StatusRequestEntityTooLarge, CodeRequestTooLarge, message, details)
}

// TooManyRequests writes a 429 Too Many Requests error response.
func TooManyRequests(w http.ResponseWriter, r *http.Request, message string) {
	if message == "" {
		message = "Too many requests"
	}
	WriteError(w, r, http.StatusTooManyRequests, CodeTooManyRequests, message, "")
}

// AccountLocked writes a 429 Too Many Requests error response for locked accounts.
func AccountLocked(w http.ResponseWriter, r *http.Request, message string) {
	if message == "" {
		message = "Account temporarily locked due to too many failed login attempts"
	}
	WriteError(w, r, http.StatusTooManyRequests, CodeAccountLocked, message, "")
}

// formatBytes formats bytes into human readable format.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%d %cB", b/div, "KMGTPE"[exp])
}
