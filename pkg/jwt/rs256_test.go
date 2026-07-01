package jwt_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
	"github.com/nbe-group/apigateway/internal/models"
	"github.com/nbe-group/apigateway/pkg/jwt"
)

// TestRS256_RoundTrip proves the RS256 path is fully wired: a token signed with
// a private key verifies against the configured public key. This is the
// "design for RS256" requirement demonstrated end-to-end.
func TestRS256_RoundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pubPEM := publicKeyPEM(t, &priv.PublicKey)

	v, err := jwt.New(jwt.Config{
		Algorithm: "RS256",
		PublicKey: pubPEM,
		Issuer:    testIssuer,
		Audience:  testAudience,
	})
	require.NoError(t, err)

	claims := models.Claims{
		Role: "admin",
		RegisteredClaims: gojwt.RegisteredClaims{
			Subject:   "rsa-user",
			Issuer:    testIssuer,
			Audience:  gojwt.ClaimStrings{testAudience},
			ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(priv)
	require.NoError(t, err)

	got, err := v.Verify(signed)
	require.NoError(t, err)
	assert.Equal(t, "rsa-user", got.UserID())
	assert.Equal(t, "admin", got.Role)
}

// TestRS256_RejectsHS256Token guards against algorithm confusion: an HS256
// token must not be accepted by an RS256 verifier.
func TestRS256_RejectsHS256Token(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	v, err := jwt.New(jwt.Config{Algorithm: "RS256", PublicKey: publicKeyPEM(t, &priv.PublicKey), Issuer: testIssuer, Audience: testAudience})
	require.NoError(t, err)

	hsToken, err := jwt.SignHS256("secret", jwt.NewClaims("u", "admin", testIssuer, testAudience, time.Hour, time.Now()))
	require.NoError(t, err)

	_, err = v.Verify(hsToken)
	assert.ErrorIs(t, err, apperrors.ErrInvalidToken)
}

func TestRS256_BadPublicKey(t *testing.T) {
	_, err := jwt.New(jwt.Config{Algorithm: "RS256", PublicKey: "garbage"})
	assert.Error(t, err)
}

func publicKeyPEM(t *testing.T, pub *rsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	require.NoError(t, err)
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	return string(pem.EncodeToMemory(block))
}
