package requestid_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/nbe-group/apigateway/internal/middleware/requestid"
	"github.com/nbe-group/apigateway/internal/transport"
)

func init() { gin.SetMode(gin.TestMode) }

func TestRequestID_GeneratedWhenAbsent(t *testing.T) {
	r := gin.New()
	r.Use(requestid.New())
	var seen string
	r.GET("/", func(c *gin.Context) { seen = transport.RequestID(c); c.Status(200) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.NotEmpty(t, seen)
	assert.Equal(t, seen, w.Header().Get(transport.HeaderRequestID), "id echoed to client")
}

func TestRequestID_PreservesInbound(t *testing.T) {
	r := gin.New()
	r.Use(requestid.New())
	var seen string
	r.GET("/", func(c *gin.Context) { seen = transport.RequestID(c); c.Status(200) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(transport.HeaderRequestID, "inbound-id-123")
	r.ServeHTTP(w, req)

	assert.Equal(t, "inbound-id-123", seen)
	assert.Equal(t, "inbound-id-123", w.Header().Get(transport.HeaderRequestID))
}
