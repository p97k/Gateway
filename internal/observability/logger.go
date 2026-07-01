package observability

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/nbe-group/apigateway/internal/config"
)

// NewLogger builds a zap.Logger from config. JSON format is the production
// default (machine-parseable, ingestion-friendly); console format is friendlier
// for local development.
func NewLogger(cfg config.LoggingConfig) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(orDefault(cfg.Level, "info"))
	if err != nil {
		return nil, fmt.Errorf("parse log level %q: %w", cfg.Level, err)
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encCfg.EncodeLevel = zapcore.LowercaseLevelEncoder

	zcfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(level),
		Encoding:         encoding(cfg.Format),
		EncoderConfig:    encCfg,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}
	return zcfg.Build()
}

func encoding(format string) string {
	if format == "console" {
		return "console"
	}
	return "json"
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
