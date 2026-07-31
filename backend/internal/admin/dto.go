// Package admin owns the admins table and the JWT authentication that guards
// every /admin/* route.
package admin

import "time"

// LoginRequest is the body of POST /api/v1/admin/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse carries the access token and when it expires, so the client does
// not have to decode the token to know.
type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
