package admin

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims is the verified identity carried by an admin access token.
type Claims struct {
	AdminID uuid.UUID
	Email   string
}

// TokenIssuer issues and verifies the HMAC-signed admin access tokens.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenIssuer builds an issuer signing with HS256 and the given lifetime.
func NewTokenIssuer(secret string, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{secret: []byte(secret), ttl: ttl}
}

// Issue mints a token for an authenticated admin and reports when it expires, so
// the login response can tell the client without decoding the token itself.
func (t *TokenIssuer) Issue(adminID uuid.UUID, email string) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(t.ttl)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   adminID.String(),
		"email": email,
		"iat":   now.Unix(),
		"exp":   expiresAt.Unix(),
	})

	signed, err := token.SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign admin token: %w", err)
	}
	return signed, expiresAt, nil
}

// Verify parses and validates a token, returning its claims. Only HS256 is
// accepted: allowing the token's own header to select the algorithm is what makes
// algorithm-confusion forgeries possible.
func (t *TokenIssuer) Verify(raw string) (*Claims, error) {
	parsed, err := jwt.Parse(raw, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %q", token.Header["alg"])
		}
		return t.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return nil, fmt.Errorf("verify admin token: %w", err)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("verify admin token: unexpected claims type")
	}

	subject, err := claims.GetSubject()
	if err != nil {
		return nil, fmt.Errorf("verify admin token: %w", err)
	}
	adminID, err := uuid.Parse(subject)
	if err != nil {
		return nil, fmt.Errorf("verify admin token: subject is not a UUID: %w", err)
	}

	email, _ := claims["email"].(string)
	return &Claims{AdminID: adminID, Email: email}, nil
}
