package response

import (
	"encoding/json"
	"net/http"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
)

// WriteError writes the standard error envelope to a raw http.ResponseWriter.
// It exists for code paths that operate outside the gin context — notably the
// reverse proxy's ErrorHandler, which is called by net/http/httputil with a
// plain ResponseWriter.
func WriteError(w http.ResponseWriter, err error) {
	apiErr := apperrors.From(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(apiErr.HTTPStatus)
	_ = json.NewEncoder(w).Encode(Envelope{
		Success: false,
		Message: apiErr.Message,
		Code:    apiErr.Code,
	})
}
