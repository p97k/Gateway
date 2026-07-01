// Command token mints a demo HS256 JWT for exercising the gateway locally.
//
// The gateway itself never issues tokens (that is the auth service's job); this
// helper just saves you from reaching for jwt.io during development.
//
// Example:
//
//	go run ./cmd/token -sub user-1 -role customer
//	curl -H "Authorization: Bearer $(go run ./cmd/token -role admin)" \
//	     http://localhost:8080/api/admin/x
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nbe-group/apigateway/pkg/jwt"
)

func main() {
	secret := flag.String("secret", "secret123", "HMAC secret (must match gateway jwt.secret)")
	issuer := flag.String("iss", "auth-service", "issuer (must match gateway jwt.issuer)")
	audience := flag.String("aud", "api", "audience (must match gateway jwt.audience)")
	sub := flag.String("sub", "user-123", "subject / user id")
	role := flag.String("role", "customer", "role claim")
	ttl := flag.Duration("ttl", time.Hour, "token time-to-live")
	flag.Parse()

	claims := jwt.NewClaims(*sub, *role, *issuer, *audience, *ttl, time.Now())
	token, err := jwt.SignHS256(*secret, claims)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println(token)
}
