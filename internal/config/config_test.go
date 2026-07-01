package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nbe-group/apigateway/internal/config"
)

const validYAML = `
server:
  port: 8080
jwt:
  algorithm: HS256
  secret: s3cr3t
  issuer: auth
  audience: api
routes:
  - prefix: /api
    service: svc
    auth: false
  - prefix: /api/admin
    service: svc
    auth: true
    roles: [admin]
services:
  svc:
    url: http://localhost:9000
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoad_Valid(t *testing.T) {
	cfg, err := config.Load(writeTemp(t, validYAML))
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "HS256", cfg.JWT.Algorithm)
}

func TestLoad_SortsRoutesLongestPrefixFirst(t *testing.T) {
	cfg, err := config.Load(writeTemp(t, validYAML))
	require.NoError(t, err)
	// /api/admin (10) must come before /api (4).
	require.GreaterOrEqual(t, len(cfg.Routes), 2)
	assert.Equal(t, "/api/admin", cfg.Routes[0].Prefix)
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("GATEWAY_SERVER_PORT", "9999")
	cfg, err := config.Load(writeTemp(t, validYAML))
	require.NoError(t, err)
	assert.Equal(t, 9999, cfg.Server.Port)
}

func TestValidate_RejectsUnknownService(t *testing.T) {
	const badYAML = `
server: {port: 8080}
jwt: {algorithm: HS256, secret: x}
routes:
  - prefix: /api
    service: missing
services:
  svc: {url: http://localhost:9000}
`
	_, err := config.Load(writeTemp(t, badYAML))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not defined")
}

func TestValidate_RejectsRolesWithoutAuth(t *testing.T) {
	const badYAML = `
server: {port: 8080}
jwt: {algorithm: HS256, secret: x}
routes:
  - prefix: /api
    service: svc
    auth: false
    roles: [admin]
services:
  svc: {url: http://localhost:9000}
`
	_, err := config.Load(writeTemp(t, badYAML))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "roles")
}

func TestValidate_RejectsHS256WithoutSecret(t *testing.T) {
	const badYAML = `
server: {port: 8080}
jwt: {algorithm: HS256}
routes:
  - prefix: /api
    service: svc
    auth: true
services:
  svc: {url: http://localhost:9000}
`
	_, err := config.Load(writeTemp(t, badYAML))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret")
}

func TestValidate_RejectsBadPort(t *testing.T) {
	const badYAML = `
server: {port: 70000}
jwt: {algorithm: HS256, secret: x}
services:
  svc: {url: http://localhost:9000}
`
	_, err := config.Load(writeTemp(t, badYAML))
	require.Error(t, err)
}
