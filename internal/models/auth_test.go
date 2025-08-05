package models_test

import (
	"testing"
	"time"
	
	"github.com/stretchr/testify/assert"
	
	"github.com/TheMichaelB/obsync/internal/models"
)

func TestTokenInfo_IsExpired(t *testing.T) {
	now := time.Now()
	
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{
			name:      "not expired",
			expiresAt: now.Add(time.Hour),
			want:      false,
		},
		{
			name:      "expired",
			expiresAt: now.Add(-time.Hour),
			want:      true,
		},
		{
			name:      "just expired",
			expiresAt: now.Add(-time.Second),
			want:      true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := &models.TokenInfo{
				Token:     "test-token",
				ExpiresAt: tt.expiresAt,
				Email:     "test@example.com",
			}
			
			got := token.IsExpired()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAuthRequest_Structure(t *testing.T) {
	req := &models.AuthRequest{
		Email:    "user@example.com",
		Password: "secret123",
		TOTP:     "123456",
	}
	
	assert.Equal(t, "user@example.com", req.Email)
	assert.Equal(t, "secret123", req.Password)
	assert.Equal(t, "123456", req.TOTP)
}

func TestAuthResponse_Structure(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	resp := &models.AuthResponse{
		Token:     "jwt-token-here",
		ExpiresAt: expiry,
		UserID:    "user-123",
	}
	
	assert.Equal(t, "jwt-token-here", resp.Token)
	assert.Equal(t, expiry, resp.ExpiresAt)
	assert.Equal(t, "user-123", resp.UserID)
}