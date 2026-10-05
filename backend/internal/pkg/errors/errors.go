package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// ============================================================================
// APP ERROR
// ============================================================================

// AppError represents a structured application error
type AppError struct {
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	StatusCode int               `json:"status_code"`
	Details    map[string]string `json:"details,omitempty"`
	Cause      error             `json:"-"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause
func (e *AppError) Unwrap() error {
	return e.Cause
}

// ============================================================================
// ERROR CONSTRUCTORS
// ============================================================================

// New creates a new AppError
func New(code, message string, statusCode int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
	}
}

// Wrap wraps an existing error with context
func Wrap(err error, code, message string, statusCode int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
		Cause:      err,
	}
}

// WithDetails adds field-level details to an error
func (e *AppError) WithDetails(details map[string]string) *AppError {
	e.Details = details
	return e
}

// ============================================================================
// COMMON ERRORS
// ============================================================================

// Authentication errors
var (
	ErrUnauthorized      = New("UNAUTHORIZED", "authentication required", http.StatusUnauthorized)
	ErrInvalidToken      = New("INVALID_TOKEN", "invalid or expired token", http.StatusUnauthorized)
	ErrTokenExpired      = New("TOKEN_EXPIRED", "token has expired", http.StatusUnauthorized)
	ErrInvalidCredentials = New("INVALID_CREDENTIALS", "invalid email or password", http.StatusUnauthorized)
	ErrForbidden         = New("FORBIDDEN", "insufficient permissions", http.StatusForbidden)
)

// Validation errors
var (
	ErrBadRequest        = New("BAD_REQUEST", "invalid request", http.StatusBadRequest)
	ErrValidation        = New("VALIDATION_ERROR", "validation failed", http.StatusUnprocessableEntity)
	ErrInvalidEmail      = New("INVALID_EMAIL", "invalid email format", http.StatusBadRequest)
	ErrInvalidPassword   = New("INVALID_PASSWORD", "password does not meet requirements", http.StatusBadRequest)
	ErrWeakPassword      = New("WEAK_PASSWORD", "password is too weak", http.StatusBadRequest)
	ErrInvalidPhone      = New("INVALID_PHONE", "invalid phone number", http.StatusBadRequest)
	ErrMissingField      = New("MISSING_FIELD", "required field is missing", http.StatusBadRequest)
)

// Resource errors
var (
	ErrNotFound          = New("NOT_FOUND", "resource not found", http.StatusNotFound)
	ErrUserNotFound      = New("USER_NOT_FOUND", "user not found", http.StatusNotFound)
	ErrRideNotFound      = New("RIDE_NOT_FOUND", "ride not found", http.StatusNotFound)
	ErrDriverNotFound    = New("DRIVER_NOT_FOUND", "driver not found", http.StatusNotFound)
	ErrHotelNotFound     = New("HOTEL_NOT_FOUND", "hotel not found", http.StatusNotFound)
	ErrRestaurantNotFound = New("RESTAURANT_NOT_FOUND", "restaurant not found", http.StatusNotFound)
	ErrConflict          = New("CONFLICT", "resource conflict", http.StatusConflict)
	ErrEmailExists       = New("EMAIL_EXISTS", "email already registered", http.StatusConflict)
)

// Business logic errors
var (
	ErrNoDriversAvailable = New("NO_DRIVERS", "no drivers available", http.StatusServiceUnavailable)
	ErrRideNotCancellable = New("RIDE_NOT_CANCELLABLE", "ride cannot be cancelled", http.StatusBadRequest)
	ErrRideNotRatable     = New("RIDE_NOT_RATABLE", "ride cannot be rated", http.StatusBadRequest)
	ErrPaymentFailed      = New("PAYMENT_FAILED", "payment processing failed", http.StatusPaymentRequired)
	ErrInsufficientFunds  = New("INSUFFICIENT_FUNDS", "insufficient funds", http.StatusPaymentRequired)
	ErrInvalidLocation    = New("INVALID_LOCATION", "invalid location", http.StatusBadRequest)
	ErrSameLocation       = New("SAME_LOCATION", "pickup and dropoff must be different", http.StatusBadRequest)
)

// System errors
var (
	ErrInternal          = New("INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
	ErrDatabase          = New("DATABASE_ERROR", "database operation failed", http.StatusInternalServerError)
	ErrExternalService   = New("EXTERNAL_SERVICE_ERROR", "external service unavailable", http.StatusBadGateway)
	ErrTimeout           = New("TIMEOUT", "request timeout", http.StatusRequestTimeout)
	ErrRateLimited       = New("RATE_LIMITED", "too many requests", http.StatusTooManyRequests)
)

// ============================================================================
// ERROR CHECKING
// ============================================================================

// Is checks if an error matches a target
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As extracts an error into a target type
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

// GetStatusCode extracts HTTP status code from error
func GetStatusCode(err error) int {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.StatusCode
	}
	return http.StatusInternalServerError
}

// GetCode extracts error code from error
func GetCode(err error) string {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return "UNKNOWN_ERROR"
}

// GetMessage extracts error message from error
func GetMessage(err error) string {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return err.Error()
}

// ============================================================================
// ERROR RESPONSE
// ============================================================================

// ErrorResponse is the JSON response format for errors
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error details
type ErrorDetail struct {
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	Details    map[string]string `json:"details,omitempty"`
	RequestID  string            `json:"request_id,omitempty"`
}

// ToResponse converts AppError to ErrorResponse
func (e *AppError) ToResponse(requestID string) *ErrorResponse {
	return &ErrorResponse{
		Error: ErrorDetail{
			Code:      e.Code,
			Message:   e.Message,
			Details:   e.Details,
			RequestID: requestID,
		},
	}
}

// ============================================================================
// ERROR WRAPPING HELPERS
// ============================================================================

// WrapNotFound wraps an error as not found
func WrapNotFound(err error, resource string) *AppError {
	return Wrap(err, "NOT_FOUND", fmt.Sprintf("%s not found", resource), http.StatusNotFound)
}

// WrapValidation wraps an error as validation error
func WrapValidation(err error, field string) *AppError {
	return Wrap(err, "VALIDATION_ERROR", fmt.Sprintf("invalid %s", field), http.StatusUnprocessableEntity)
}

// WrapDatabase wraps a database error
func WrapDatabase(err error, operation string) *AppError {
	return Wrap(err, "DATABASE_ERROR", fmt.Sprintf("database %s failed", operation), http.StatusInternalServerError)
}