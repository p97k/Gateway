// Package jwt provides a small, framework-agnostic JWT verification
// abstraction for the gateway.
//
// Design goals:
//   - The rest of the gateway depends on the Verifier interface, never on the
//     concrete signing scheme. This is the Dependency Inversion Principle: the
//     auth middleware asks "is this token valid?" without knowing HOW.
//   - Adding RS256 (or ES256, JWKS rotation, etc.) means adding a new
//     keyProvider — no caller changes. HS256 is implemented today; RS256 has a
//     clearly marked extension point.
package jwt

import (
	"crypto/rsa"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	apperrors "github.com/nbe-group/apigateway/internal/errors"
	"github.com/nbe-group/apigateway/internal/models"
)

// Verifier validates a raw token string and returns the decoded claims.
// Implementations must validate signature, expiry, issuer and audience.
type Verifier interface {
	Verify(tokenString string) (*models.Claims, error)
}

// Config configures the default verifier.
type Config struct {
	Algorithm string // "HS256" | "RS256"
	Secret    string // HS256 HMAC secret
	PublicKey string // RS256 PEM-encoded public key
	Issuer    string // expected iss
	Audience  string // expected aud
}

// keyProvider abstracts the signature-scheme-specific concerns: which signing
// methods are acceptable and which key validates them. This is the strategy
// that makes new algorithms drop-in.
type keyProvider interface {
	// keyFunc returns the jwt.Keyfunc used to resolve the verification key. It
	// must reject tokens whose alg header does not match the expected method,
	// which defends against algorithm-confusion attacks (e.g. an attacker
	// presenting an HS256 token signed with the RSA public key as the secret).
	keyFunc() jwt.Keyfunc
	// validMethods returns the alg names the parser will accept.
	validMethods() []string
}

// service is the default Verifier implementation.
type service struct {
	provider keyProvider
	issuer   string
	audience string
}

// New constructs a Verifier from config, selecting the key provider by
// algorithm. Returns an error for unsupported or misconfigured algorithms.
func New(cfg Config) (Verifier, error) {
	var provider keyProvider
	switch cfg.Algorithm {
	case "HS256", "":
		if cfg.Secret == "" {
			return nil, errors.New("jwt: HS256 requires a non-empty secret")
		}
		provider = &hmacProvider{secret: []byte(cfg.Secret)}
	case "RS256":
		key, err := parseRSAPublicKey(cfg.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("jwt: parse RS256 public key: %w", err)
		}
		provider = &rsaProvider{publicKey: key}
	default:
		return nil, fmt.Errorf("jwt: unsupported algorithm %q", cfg.Algorithm)
	}

	return &service{
		provider: provider,
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
	}, nil
}

// Verify parses and validates the token, mapping library errors onto the
// gateway's typed error catalog so the HTTP layer can render precise codes.
func (s *service) Verify(tokenString string) (*models.Claims, error) {
	if tokenString == "" {
		return nil, apperrors.ErrMissingToken
	}

	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods(s.provider.validMethods()),
		jwt.WithExpirationRequired(),
	}
	if s.issuer != "" {
		parserOpts = append(parserOpts, jwt.WithIssuer(s.issuer))
	}
	if s.audience != "" {
		parserOpts = append(parserOpts, jwt.WithAudience(s.audience))
	}

	claims := &models.Claims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, s.provider.keyFunc(), parserOpts...)
	if err != nil {
		return nil, mapError(err)
	}
	return claims, nil
}

// mapError translates jwt v5 sentinel errors into the gateway's error catalog.
func mapError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return apperrors.ErrExpiredToken.WithError(err)
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return apperrors.ErrInvalidIssuer.WithError(err)
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return apperrors.ErrInvalidAudience.WithError(err)
	case errors.Is(err, jwt.ErrTokenNotValidYet),
		errors.Is(err, jwt.ErrTokenMalformed),
		errors.Is(err, jwt.ErrTokenSignatureInvalid),
		errors.Is(err, jwt.ErrTokenUnverifiable):
		return apperrors.ErrInvalidToken.WithError(err)
	default:
		return apperrors.ErrInvalidToken.WithError(err)
	}
}

// hmacProvider implements HS256 verification.
type hmacProvider struct{ secret []byte }

func (p *hmacProvider) validMethods() []string { return []string{"HS256"} }

func (p *hmacProvider) keyFunc() jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return p.secret, nil
	}
}

// rsaProvider implements RS256 verification. It is fully wired; enabling it is
// purely a configuration concern (set jwt.algorithm: RS256 and jwt.public_key).
type rsaProvider struct{ publicKey *rsa.PublicKey }

func (p *rsaProvider) validMethods() []string { return []string{"RS256"} }

func (p *rsaProvider) keyFunc() jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return p.publicKey, nil
	}
}

func parseRSAPublicKey(pem string) (*rsa.PublicKey, error) {
	if pem == "" {
		return nil, errors.New("empty public key")
	}
	return jwt.ParseRSAPublicKeyFromPEM([]byte(pem))
}
