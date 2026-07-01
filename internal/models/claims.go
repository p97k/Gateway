// Package models holds the gateway's plain data structures that are shared
// across layers. These types intentionally have no behavior and no
// dependencies on infrastructure, keeping them at the center of the
// architecture (the "entities" layer in Clean Architecture terms).
package models

import "github.com/golang-jwt/jwt/v5"

// Claims is the gateway's view of an authenticated principal, decoded from a
// validated JWT. Only the fields the gateway needs for authorization and
// header propagation are modeled explicitly; everything else stays in the raw
// token and is ignored.
//
// It embeds jwt.RegisteredClaims so standard fields (iss, aud, exp, sub, iat,
// nbf) are validated by the jwt library, while Role is our custom claim.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// UserID returns the subject (sub) claim, which the gateway treats as the
// canonical user identifier propagated downstream as X-User-Id.
func (c *Claims) UserID() string {
	if c == nil {
		return ""
	}
	return c.Subject
}
