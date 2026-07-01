// Package requestid provides the first middleware in the chain: it ensures
// every request carries a stable correlation id.
//
// The id is read from an inbound X-Request-Id header when present (so a caller
// or upstream proxy can propagate its own id), otherwise a new UUID is minted.
// The id is stored on the context, echoed back to the client, and later
// injected into the upstream request and every log line — this is what ties a
// single client request to all the work it triggers.
package requestid

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/nbe-group/apigateway/internal/transport"
)

// New returns the request-id middleware.
func New() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(transport.HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		transport.SetRequestID(c, id)
		// Echo to the client so they can report it when something goes wrong.
		c.Header(transport.HeaderRequestID, id)
		c.Next()
	}
}
