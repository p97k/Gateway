package service_registry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
	"github.com/nbe-group/apigateway/internal/service_registry"
)

func TestStatic_Resolve(t *testing.T) {
	reg, err := service_registry.NewStatic(map[string]string{
		"product": "http://localhost:8081",
	})
	require.NoError(t, err)

	inst, err := reg.Resolve(context.Background(), "product")
	require.NoError(t, err)
	assert.Equal(t, "localhost:8081", inst.URL.Host)
}

func TestStatic_UnknownService(t *testing.T) {
	reg, err := service_registry.NewStatic(map[string]string{"product": "http://localhost:8081"})
	require.NoError(t, err)

	_, err = reg.Resolve(context.Background(), "missing")
	assert.ErrorIs(t, err, apperrors.ErrServiceUnknown)
}

func TestStatic_RejectsBadURL(t *testing.T) {
	_, err := service_registry.NewStatic(map[string]string{"x": "://no-scheme"})
	assert.Error(t, err)
}

func TestStatic_SetHealth(t *testing.T) {
	reg, err := service_registry.NewStatic(map[string]string{"product": "http://localhost:8081"})
	require.NoError(t, err)

	reg.SetHealth("product", false)
	_, err = reg.Resolve(context.Background(), "product")
	assert.ErrorIs(t, err, apperrors.ErrServiceNoTarget)

	reg.SetHealth("product", true)
	_, err = reg.Resolve(context.Background(), "product")
	assert.NoError(t, err)
}

func TestStatic_Services(t *testing.T) {
	reg, err := service_registry.NewStatic(map[string]string{
		"b": "http://localhost:2", "a": "http://localhost:1",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, reg.Services())
}
