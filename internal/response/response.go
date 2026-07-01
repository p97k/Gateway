// Package response centralizes how the gateway writes JSON responses and
// errors to clients. Keeping this in one place guarantees every endpoint and
// middleware emits the exact same envelope, which is what the task's error
// contract requires:
//
//	{ "success": false, "message": "Unauthorized", "code": "AUTH_001" }
package response

import (
	"github.com/gin-gonic/gin"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
)

// Envelope is the standard response body for gateway-originated responses
// (errors and gateway endpoints such as /health). Proxied responses are passed
// through untouched and do NOT use this envelope — the gateway must not rewrite
// upstream payloads.
type Envelope struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// OK writes a success envelope with optional data.
func OK(c *gin.Context, status int, data any) {
	c.JSON(status, Envelope{Success: true, Data: data})
}

// Error writes the standard error envelope derived from an error. Any error is
// normalized through apperrors.From so non-API errors are surfaced as a generic
// internal error and never leak details to the client. The matching error code
// is also stored on the gin context (errorCodeKey) so the logging and metrics
// middleware can record it without re-parsing.
func Error(c *gin.Context, err error) {
	apiErr := apperrors.From(err)
	c.Set(errorCodeKey, apiErr.Code)
	// Attach the full error to gin so the logging middleware can log the cause.
	_ = c.Error(err)
	c.AbortWithStatusJSON(apiErr.HTTPStatus, Envelope{
		Success: false,
		Message: apiErr.Message,
		Code:    apiErr.Code,
	})
}

const errorCodeKey = "gateway.error_code"

// ErrorCode returns the error code recorded by Error for the current request,
// or "" if the request did not fail with a typed error.
func ErrorCode(c *gin.Context) string {
	if v, ok := c.Get(errorCodeKey); ok {
		if code, ok := v.(string); ok {
			return code
		}
	}
	return ""
}
