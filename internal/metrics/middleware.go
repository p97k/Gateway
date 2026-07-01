package metrics

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/nbe-group/apigateway/internal/response"
	"github.com/nbe-group/apigateway/internal/transport"
)

// Middleware records request count, duration and error metrics. It sits near
// the top of the chain so its timer spans the full request (including proxy
// time), but reads the matched service name AFTER c.Next() — by which point the
// router has recorded it on the context.
func (m *Metrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		status := c.Writer.Status()
		service := transport.RouteName(c)
		m.ObserveRequest(c.Request.Method, service, status, time.Since(start).Seconds())
		m.ObserveError(service, response.ErrorCode(c))
	}
}
