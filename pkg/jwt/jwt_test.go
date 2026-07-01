package jwt_test

import (
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
	"github.com/nbe-group/apigateway/internal/models"
	"github.com/nbe-group/apigateway/pkg/jwt"
)

const (
	testSecret   = "test-secret"
	testIssuer   = "auth-service"
	testAudience = "api"
)

func newVerifier(t *testing.T) jwt.Verifier {
	t.Helper()
	v, err := jwt.New(jwt.Config{
		Algorithm: "HS256",
		Secret:    testSecret,
		Issuer:    testIssuer,
		Audience:  testAudience,
	})
	require.NoError(t, err)
	return v
}

func TestVerify_ValidToken(t *testing.T) {
	v := newVerifier(t)
	claims := jwt.NewClaims("user-123", "customer", testIssuer, testAudience, time.Hour, time.Now())
	token, err := jwt.SignHS256(testSecret, claims)
	require.NoError(t, err)

	got, err := v.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", got.UserID())
	assert.Equal(t, "customer", got.Role)
}

func TestVerify_EmptyToken(t *testing.T) {
	v := newVerifier(t)
	_, err := v.Verify("")
	assert.ErrorIs(t, err, apperrors.ErrMissingToken)
}

func TestVerify_ExpiredToken(t *testing.T) {
	v := newVerifier(t)
	// Issued and expired an hour ago.
	claims := jwt.NewClaims("u", "customer", testIssuer, testAudience, -time.Minute, time.Now().Add(-time.Hour))
	token, err := jwt.SignHS256(testSecret, claims)
	require.NoError(t, err)

	_, err = v.Verify(token)
	assert.ErrorIs(t, err, apperrors.ErrExpiredToken)
}

func TestVerify_WrongIssuer(t *testing.T) {
	v := newVerifier(t)
	claims := jwt.NewClaims("u", "customer", "evil-issuer", testAudience, time.Hour, time.Now())
	token, err := jwt.SignHS256(testSecret, claims)
	require.NoError(t, err)

	_, err = v.Verify(token)
	assert.ErrorIs(t, err, apperrors.ErrInvalidIssuer)
}

func TestVerify_WrongAudience(t *testing.T) {
	v := newVerifier(t)
	claims := jwt.NewClaims("u", "customer", testIssuer, "other-api", time.Hour, time.Now())
	token, err := jwt.SignHS256(testSecret, claims)
	require.NoError(t, err)

	_, err = v.Verify(token)
	assert.ErrorIs(t, err, apperrors.ErrInvalidAudience)
}

func TestVerify_BadSignature(t *testing.T) {
	v := newVerifier(t)
	claims := jwt.NewClaims("u", "customer", testIssuer, testAudience, time.Hour, time.Now())
	token, err := jwt.SignHS256("a-different-secret", claims)
	require.NoError(t, err)

	_, err = v.Verify(token)
	assert.ErrorIs(t, err, apperrors.ErrInvalidToken)
}

func TestVerify_Malformed(t *testing.T) {
	v := newVerifier(t)
	_, err := v.Verify("not.a.jwt")
	assert.ErrorIs(t, err, apperrors.ErrInvalidToken)
}

// TestVerify_AlgorithmConfusion ensures a token signed with a non-HMAC alg (or
// "none") is rejected — the parser is locked to the expected method.
func TestVerify_RejectsNoneAlg(t *testing.T) {
	v := newVerifier(t)
	claims := models.Claims{RegisteredClaims: gojwt.RegisteredClaims{
		Subject:   "u",
		Issuer:    testIssuer,
		Audience:  gojwt.ClaimStrings{testAudience},
		ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	tok := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims)
	signed, err := tok.SignedString(gojwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = v.Verify(signed)
	assert.ErrorIs(t, err, apperrors.ErrInvalidToken)
}

func TestNew_HS256RequiresSecret(t *testing.T) {
	_, err := jwt.New(jwt.Config{Algorithm: "HS256"})
	assert.Error(t, err)
}

func TestNew_UnsupportedAlgorithm(t *testing.T) {
	_, err := jwt.New(jwt.Config{Algorithm: "ES512", Secret: "x"})
	assert.Error(t, err)
}
