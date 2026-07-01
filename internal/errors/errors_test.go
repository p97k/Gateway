package errors_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
)

func TestFrom_PassesThroughAPIError(t *testing.T) {
	got := apperrors.From(apperrors.ErrForbidden)
	assert.Equal(t, apperrors.ErrForbidden.Code, got.Code)
}

func TestFrom_WrapsPlainError(t *testing.T) {
	got := apperrors.From(errors.New("boom"))
	assert.Equal(t, apperrors.ErrInternal.Code, got.Code)
	assert.Equal(t, http.StatusInternalServerError, got.HTTPStatus)
}

func TestFrom_Nil(t *testing.T) {
	assert.Nil(t, apperrors.From(nil))
}

func TestWithError_PreservesSentinelIdentity(t *testing.T) {
	cause := errors.New("root cause")
	err := apperrors.ErrExpiredToken.WithError(cause)

	// Clone still matches the sentinel via Is, and the cause is unwrappable.
	assert.ErrorIs(t, err, apperrors.ErrExpiredToken)
	assert.ErrorIs(t, err, cause)
	// The original sentinel is not mutated.
	assert.Nil(t, apperrors.ErrExpiredToken.Err)
}

func TestWithMessage_OverridesMessageKeepsCode(t *testing.T) {
	err := apperrors.ErrForbidden.WithMessage("role %q denied", "guest")
	assert.Equal(t, "role \"guest\" denied", err.Message)
	assert.Equal(t, apperrors.ErrForbidden.Code, err.Code)
}

func TestError_String(t *testing.T) {
	assert.Contains(t, apperrors.ErrRateLimited.Error(), apperrors.ErrRateLimited.Code)
	withCause := apperrors.ErrInternal.WithError(errors.New("db down"))
	assert.Contains(t, withCause.Error(), "db down")
}

func TestIs_DifferentCodesDoNotMatch(t *testing.T) {
	assert.NotErrorIs(t, apperrors.ErrForbidden, apperrors.ErrRateLimited)
}
