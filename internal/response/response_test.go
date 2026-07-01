package response_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
	"github.com/nbe-group/apigateway/internal/response"
)

func init() { gin.SetMode(gin.TestMode) }

func TestError_WritesEnvelopeAndRecordsCode(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	response.Error(c, apperrors.ErrForbidden)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), `"success":false`)
	assert.Contains(t, w.Body.String(), apperrors.ErrForbidden.Code)
	assert.Equal(t, apperrors.ErrForbidden.Code, response.ErrorCode(c))
	assert.True(t, c.IsAborted())
}

func TestOK_WritesSuccessEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	response.OK(c, http.StatusOK, gin.H{"hello": "world"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"success":true`)
	assert.Contains(t, w.Body.String(), "world")
}

func TestWriteError_RawWriter(t *testing.T) {
	w := httptest.NewRecorder()
	response.WriteError(w, apperrors.ErrUpstreamTimeout)

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	assert.Contains(t, w.Body.String(), apperrors.ErrUpstreamTimeout.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

func TestErrorCode_EmptyWhenNoError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	assert.Empty(t, response.ErrorCode(c))
}
