// Package logging provides structured per-request access logging via zap.
//
// It captures the request start time, lets the rest of the chain run, then
// emits a single structured line with the fields the task requires:
// request_id, user_id, method, path, status, latency_ms, service. Emitting one
// line per request (rather than several) keeps logs cheap and easy to query.
package logging

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/nbe-group/apigateway/internal/response"
	"github.com/nbe-group/apigateway/internal/transport"
)

// New returns the access-log middleware bound to the given logger.
func New(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		fields := []zap.Field{
			zap.String("request_id", transport.RequestID(c)),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Int64("latency_ms", latency.Milliseconds()),
			zap.String("service", transport.RouteName(c)),
			zap.String("client_ip", c.ClientIP()),
		}
		if query != "" {
			fields = append(fields, zap.String("query", query))
		}
		if claims := transport.Claims(c); claims != nil {
			fields = append(fields, zap.String("user_id", claims.UserID()))
			fields = append(fields, zap.String("user_role", claims.Role))
		}
		if code := response.ErrorCode(c); code != "" {
			fields = append(fields, zap.String("error_code", code))
		}
		// gin collects handler errors in c.Errors; surface the last cause.
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("error", c.Errors.Last().Err.Error()))
		}

		logger.Log(levelForStatus(status), "request", fields...)
	}
}

// levelForStatus maps HTTP status classes to log levels so dashboards can alert
// on warn/error volume: 5xx -> error, 4xx -> warn, else info.
func levelForStatus(status int) zapcore.Level {
	switch {
	case status >= 500:
		return zapcore.ErrorLevel
	case status >= 400:
		return zapcore.WarnLevel
	default:
		return zapcore.InfoLevel
	}
}
