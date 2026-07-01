// Package errors defines the gateway's domain error type and a catalog of
// well-known errors. Centralizing errors here lets every layer return a
// consistent, typed error that the HTTP layer can translate into the standard
// API envelope (see internal/response) without leaking internal details.
package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError is the canonical error type used across the gateway.
//
// It carries everything the presentation layer needs to render a response:
//   - HTTPStatus: the status code to send to the client.
//   - Code: a stable, machine-readable error code (e.g. "AUTH_001"). Clients
//     and dashboards key off this, so codes must remain stable over time.
//   - Message: a human-readable, client-safe message. Never put secrets or
//     internal stack details here.
//   - Err: the wrapped underlying error (optional). Used for logging only; it
//     is never serialized to the client.
//
// APIError implements the error interface and supports errors.Is/As/Unwrap so
// it composes cleanly with the standard library.
type APIError struct {
	HTTPStatus int
	Code       string
	Message    string
	Err        error
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s (%s): %v", e.Message, e.Code, e.Err)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

// Unwrap exposes the wrapped error for errors.Is / errors.As.
func (e *APIError) Unwrap() error { return e.Err }

// Is reports whether target is the same logical error, comparing by the stable
// Code. This is what makes errors.Is(err, ErrExpiredToken) work even after
// WithError/WithMessage produced a clone of the sentinel — identity is the
// code, not the pointer.
func (e *APIError) Is(target error) bool {
	var t *APIError
	if !errors.As(target, &t) {
		return false
	}
	return e.Code == t.Code
}

// WithError returns a copy of the error with an underlying cause attached.
// The original sentinel is left untouched so the package-level catalog values
// remain immutable and safe to compare with errors.Is.
func (e *APIError) WithError(err error) *APIError {
	clone := *e
	clone.Err = err
	return &clone
}

// WithMessage returns a copy of the error with an overridden client message
// while preserving the code and status. Useful for adding request-specific
// context (e.g. which field failed validation).
func (e *APIError) WithMessage(format string, args ...any) *APIError {
	clone := *e
	clone.Message = fmt.Sprintf(format, args...)
	return &clone
}

// New constructs an ad-hoc APIError.
func New(httpStatus int, code, message string) *APIError {
	return &APIError{HTTPStatus: httpStatus, Code: code, Message: message}
}

// From normalizes an arbitrary error into an *APIError. If err already is (or
// wraps) an *APIError it is returned as-is; otherwise it is treated as an
// internal error so we never accidentally leak raw error strings to clients.
func From(err error) *APIError {
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return ErrInternal.WithError(err)
}

// Catalog of well-known errors. The codes form a small namespaced taxonomy:
//
//	AUTH_*  authentication / authorization failures
//	RATE_*  rate limiting
//	RTE_*   routing / service resolution
//	REQ_*   request validation
//	PRX_*   upstream proxy failures
//	GW_*    generic gateway / internal failures
var (
	// Authentication & authorization.
	ErrMissingToken    = New(http.StatusUnauthorized, "AUTH_001", "Authorization token is required")
	ErrInvalidToken    = New(http.StatusUnauthorized, "AUTH_002", "Invalid or malformed token")
	ErrExpiredToken    = New(http.StatusUnauthorized, "AUTH_003", "Token has expired")
	ErrInvalidIssuer   = New(http.StatusUnauthorized, "AUTH_004", "Token issuer is not trusted")
	ErrInvalidAudience = New(http.StatusUnauthorized, "AUTH_005", "Token audience is not accepted")
	ErrForbidden       = New(http.StatusForbidden, "AUTH_006", "You do not have permission to access this resource")

	// Rate limiting.
	ErrRateLimited = New(http.StatusTooManyRequests, "RATE_001", "Too many requests")

	// Routing / service resolution.
	ErrRouteNotFound   = New(http.StatusNotFound, "RTE_001", "No route matches the requested path")
	ErrServiceUnknown  = New(http.StatusBadGateway, "RTE_002", "Target service is not registered")
	ErrServiceNoTarget = New(http.StatusServiceUnavailable, "RTE_003", "No healthy upstream available for service")

	// Request validation.
	ErrBadRequest = New(http.StatusBadRequest, "REQ_001", "The request is invalid")

	// Proxying.
	ErrUpstreamUnavailable = New(http.StatusBadGateway, "PRX_001", "Upstream service is unavailable")
	ErrUpstreamTimeout     = New(http.StatusGatewayTimeout, "PRX_002", "Upstream service timed out")

	// Generic.
	ErrInternal = New(http.StatusInternalServerError, "GW_001", "An internal error occurred")
)
