package auth_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/obsync/internal/models"
	"github.com/yourusername/obsync/internal/services/auth"
	"github.com/yourusername/obsync/internal/transport"
	"github.com/yourusername/obsync/test/testutil"
)

func TestAuthService(t *testing.T) {
	logger := testutil.NewTestLogger()
	mockTransport := transport.NewMockTransport()
	tokenFile := "test_token.json"
	defer os.Remove(tokenFile)

	service := auth.NewService(mockTransport, tokenFile, logger)

	t.Run("successful login", func(t *testing.T) {
		// Mock login response
		expiry := time.Now().Add(24 * time.Hour)
		mockTransport.AddResponse("/api/v1/auth/login", map[string]interface{}{
			"token":      "test-token-123",
			"expires_at": expiry.Format(time.RFC3339),
		})

		err := service.Login(context.Background(), "user@example.com", "password", "")
		require.NoError(t, err)

		// Verify token was set
		token, err := service.GetToken()
		require.NoError(t, err)
		assert.Equal(t, "test-token-123", token.Token)
		assert.Equal(t, "user@example.com", token.Email)
		assert.False(t, token.IsExpired())

		// Verify transport token was set
		assert.Equal(t, "test-token-123", mockTransport.GetToken())
	})

	t.Run("token persistence", func(t *testing.T) {
		// Create new service to test loading
		service2 := auth.NewService(transport.NewMockTransport(), tokenFile, logger)

		token, err := service2.GetToken()
		require.NoError(t, err)
		assert.Equal(t, "test-token-123", token.Token)
	})

	t.Run("token refresh", func(t *testing.T) {
		newExpiry := time.Now().Add(48 * time.Hour)
		mockTransport.AddResponse("/api/v1/auth/refresh", map[string]interface{}{
			"token":      "refreshed-token-456",
			"expires_at": newExpiry.Format(time.RFC3339),
		})

		err := service.RefreshToken(context.Background())
		require.NoError(t, err)

		token, err := service.GetToken()
		require.NoError(t, err)
		assert.Equal(t, "refreshed-token-456", token.Token)
	})

	t.Run("logout", func(t *testing.T) {
		mockTransport.AddResponse("/api/v1/auth/logout", map[string]interface{}{
			"success": true,
		})

		err := service.Logout(context.Background())
		require.NoError(t, err)

		// Token should be cleared
		_, err = service.GetToken()
		assert.ErrorIs(t, err, models.ErrNotAuthenticated)

		// Token file should be removed
		_, err = os.Stat(tokenFile)
		assert.True(t, os.IsNotExist(err))
	})
}

func TestTokenExpiry(t *testing.T) {
	logger := testutil.NewTestLogger()
	mockTransport := transport.NewMockTransport()

	service := auth.NewService(mockTransport, "", logger)

	// Mock login with expired token
	pastTime := time.Now().Add(-1 * time.Hour)
	mockTransport.AddResponse("/api/v1/auth/login", map[string]interface{}{
		"token":      "expired-token",
		"expires_at": pastTime.Format(time.RFC3339),
	})

	err := service.Login(context.Background(), "user@example.com", "password", "")
	require.NoError(t, err)

	// Token should be considered expired
	_, err = service.GetToken()
	assert.ErrorIs(t, err, models.ErrNotAuthenticated)
}

func TestEnsureAuthenticated(t *testing.T) {
	logger := testutil.NewTestLogger()
	mockTransport := transport.NewMockTransport()

	service := auth.NewService(mockTransport, "", logger)

	t.Run("no token", func(t *testing.T) {
		err := service.EnsureAuthenticated(context.Background())
		assert.ErrorIs(t, err, models.ErrNotAuthenticated)
	})

	t.Run("valid token", func(t *testing.T) {
		// Login first
		expiry := time.Now().Add(24 * time.Hour)
		mockTransport.AddResponse("/api/v1/auth/login", map[string]interface{}{
			"token":      "valid-token",
			"expires_at": expiry.Format(time.RFC3339),
		})

		err := service.Login(context.Background(), "user@example.com", "password", "")
		require.NoError(t, err)

		err = service.EnsureAuthenticated(context.Background())
		assert.NoError(t, err)
	})

	t.Run("token expiring soon", func(t *testing.T) {
		// Login with token expiring in 2 minutes
		expiry := time.Now().Add(2 * time.Minute)
		mockTransport.AddResponse("/api/v1/auth/login", map[string]interface{}{
			"token":      "expiring-token",
			"expires_at": expiry.Format(time.RFC3339),
		})

		err := service.Login(context.Background(), "user@example.com", "password", "")
		require.NoError(t, err)

		// Mock refresh response
		newExpiry := time.Now().Add(24 * time.Hour)
		mockTransport.AddResponse("/api/v1/auth/refresh", map[string]interface{}{
			"token":      "refreshed-token",
			"expires_at": newExpiry.Format(time.RFC3339),
		})

		err = service.EnsureAuthenticated(context.Background())
		assert.NoError(t, err)

		// Should have refreshed token
		token, err := service.GetToken()
		require.NoError(t, err)
		assert.Equal(t, "refreshed-token", token.Token)
	})
}