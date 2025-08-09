package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/TheMichaelB/obsync/internal/creds"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/models"
	"github.com/TheMichaelB/obsync/internal/services/totp"
	"github.com/TheMichaelB/obsync/internal/transport"
)

// Service handles authentication operations.
type Service struct {
	transport transport.Transport
	logger    *events.Logger

	// Token cache
	token     *models.TokenInfo
	tokenFile string
	
	// Combined credentials (optional)
	creds     *creds.Combined
}

// NewService creates an auth service.
func NewService(transport transport.Transport, tokenFile string, logger *events.Logger) *Service {
	return &Service{
		transport: transport,
		tokenFile: tokenFile,
		logger:    logger.WithField("service", "auth"),
	}
}

// SetCredentials sets the combined credentials.
func (s *Service) SetCredentials(c *creds.Combined) {
	s.creds = c
}

// Login authenticates using combined credentials or provided parameters.
func (s *Service) Login(ctx context.Context, email, password, totpSecret string) error {
	// Use combined credentials if available and parameters not provided
	if s.creds != nil {
		if email == "" {
			email = s.creds.Auth.Email
		}
		if password == "" {
			password = s.creds.Auth.Password
		}
		if totpSecret == "" {
			totpSecret = s.creds.Auth.TOTPSecret
		}
	}
	
	// Validate we have required credentials
	if email == "" || password == "" {
		return fmt.Errorf("email and password required")
	}
	
	// Generate TOTP code from secret if provided
	var totpCode string
	if totpSecret != "" {
		totpService := totp.NewService()
		code, err := totpService.GenerateCode(totpSecret)
		if err != nil {
			return fmt.Errorf("generate TOTP code: %w", err)
		}
		totpCode = code
		s.logger.WithField("email", email).Info("Logging in with TOTP")
	} else {
		s.logger.WithField("email", email).Info("Logging in")
	}

	req := models.AuthRequest{
		Email:    email,
		Password: password,
		TOTP:     totpCode,
	}

	resp, err := s.transport.PostJSON(ctx, "/user/signin", req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}

	// Debug: log the response
	s.logger.WithField("response", resp).Debug("Login response received")

	// Parse response
	token, ok := resp["token"].(string)
	if !ok || token == "" {
		s.logger.WithField("response_keys", getKeys(resp)).Error("Token not found in response")
		return fmt.Errorf("invalid login response: missing token")
	}

	expiresStr, _ := resp["expires_at"].(string)
	expiresAt, _ := time.Parse(time.RFC3339, expiresStr)
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(24 * time.Hour) // Default expiry
	}

	// Store token
	s.token = &models.TokenInfo{
		Token:     token,
		ExpiresAt: expiresAt,
		Email:     email,
	}

	// Update transport
	s.transport.SetToken(token)

	// Save token to file
	if err := s.saveToken(); err != nil {
		s.logger.WithError(err).Warn("Failed to save token")
	}

	s.logger.Info("Login successful")
	return nil
}

// Logout clears authentication.
func (s *Service) Logout(ctx context.Context) error {
	s.logger.Info("Logging out")

	if s.token != nil && !s.token.IsExpired() {
		// Notify server
		_, err := s.transport.PostJSON(ctx, "/user/signout", nil)
		if err != nil {
			s.logger.WithError(err).Warn("Server logout failed")
		}
	}

	// Clear local state
	s.token = nil
	s.transport.SetToken("")

	// Remove token file
	if s.tokenFile != "" {
		_ = os.Remove(s.tokenFile)
	}

	return nil
}

// GetToken returns current token if valid.
func (s *Service) GetToken() (*models.TokenInfo, error) {
	// Try cached token
	if s.token != nil && !s.token.IsExpired() {
		return s.token, nil
	}

	// Try loading from file
	if err := s.loadToken(); err == nil && s.token != nil && !s.token.IsExpired() {
		s.transport.SetToken(s.token.Token)
		return s.token, nil
	}

	return nil, models.ErrNotAuthenticated
}

// RefreshToken refreshes the auth token.
func (s *Service) RefreshToken(ctx context.Context) error {
	token, err := s.GetToken()
	if err != nil {
		return err
	}

	s.logger.Debug("Refreshing token")

	resp, err := s.transport.PostJSON(ctx, "/user/refresh", nil)
	if err != nil {
		return fmt.Errorf("refresh request: %w", err)
	}

	// Update token
	if newToken, ok := resp["token"].(string); ok {
		token.Token = newToken
		s.transport.SetToken(newToken)
	}

	if expiresStr, ok := resp["expires_at"].(string); ok {
		if expiresAt, err := time.Parse(time.RFC3339, expiresStr); err == nil {
			token.ExpiresAt = expiresAt
		}
	}

	// Save updated token
	return s.saveToken()
}

// EnsureAuthenticated checks and refreshes auth if needed.
func (s *Service) EnsureAuthenticated(ctx context.Context) error {
	token, err := s.GetToken()
	if err != nil {
		return err
	}

	// Refresh if expiring soon
	if time.Until(token.ExpiresAt) < 5*time.Minute {
		if err := s.RefreshToken(ctx); err != nil {
			s.logger.WithError(err).Warn("Token refresh failed")
			// Continue with existing token
		}
	}

	return nil
}

// Token persistence

func (s *Service) saveToken() error {
	if s.tokenFile == "" || s.token == nil {
		return nil
	}

	data, err := json.Marshal(s.token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	// Save with restricted permissions
	return os.WriteFile(s.tokenFile, data, 0600)
}

func (s *Service) loadToken() error {
	if s.tokenFile == "" {
		return fmt.Errorf("no token file configured")
	}

	data, err := os.ReadFile(s.tokenFile)
	if err != nil {
		return fmt.Errorf("read token file: %w", err)
	}

	var token models.TokenInfo
	if err := json.Unmarshal(data, &token); err != nil {
		return fmt.Errorf("parse token: %w", err)
	}

	s.token = &token
	return nil
}

// Helper function for debugging
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}