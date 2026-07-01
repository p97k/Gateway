package observability_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nbe-group/apigateway/internal/config"
	"github.com/nbe-group/apigateway/internal/observability"
)

func TestNewLogger_JSON(t *testing.T) {
	logger, err := observability.NewLogger(config.LoggingConfig{Level: "info", Format: "json"})
	require.NoError(t, err)
	require.NotNil(t, logger)
	logger.Info("hello") // must not panic
}

func TestNewLogger_ConsoleAndDefaults(t *testing.T) {
	logger, err := observability.NewLogger(config.LoggingConfig{Format: "console"})
	require.NoError(t, err)
	require.NotNil(t, logger)
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	_, err := observability.NewLogger(config.LoggingConfig{Level: "not-a-level"})
	assert.Error(t, err)
}

func TestSetup_Disabled(t *testing.T) {
	shutdown, err := observability.Setup(context.Background(), config.TracingConfig{Enabled: false})
	require.NoError(t, err)
	assert.NoError(t, shutdown(context.Background()))
}

func TestSetup_EnabledNoExporter(t *testing.T) {
	shutdown, err := observability.Setup(context.Background(), config.TracingConfig{
		Enabled:      true,
		ServiceName:  "test-gw",
		SamplerRatio: 1.0,
	})
	require.NoError(t, err)
	defer func() { _ = shutdown(context.Background()) }()

	// The global tracer should now produce a recording span.
	_, span := observability.Tracer().Start(context.Background(), "unit-span")
	assert.True(t, span.SpanContext().IsValid())
	span.End()
}
