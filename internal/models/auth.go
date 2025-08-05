package models

import "time"

// AuthRequest for login.
type AuthRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTP     string `json:"mfa,omitempty"`
}

// AuthResponse from login.
type AuthResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	UserID    string    `json:"user_id"`
}

// TokenInfo stores authentication details.
type TokenInfo struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Email     string    `json:"email"`
}

// IsExpired checks if the token has expired.
func (t *TokenInfo) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}