package totp

import (
	"fmt"
	"time"

	"github.com/pquerna/otp/totp"
)

// Service provides TOTP (Time-based One-Time Password) functionality.
type Service interface {
	// GenerateCode generates a TOTP code from a secret.
	GenerateCode(secret string) (string, error)
	
	// ValidateCode validates a TOTP code against a secret.
	ValidateCode(secret, code string) bool
	
	// GenerateCodeAtTime generates a TOTP code for a specific time.
	GenerateCodeAtTime(secret string, t time.Time) (string, error)
}

// DefaultService implements TOTP operations.
type DefaultService struct {
	// Configuration
	period    uint      // Time step in seconds (default: 30)
	digits    uint      // Number of digits (default: 6)
	algorithm string    // Hash algorithm (default: SHA1)
}

// NewService creates a new TOTP service with default settings.
func NewService() *DefaultService {
	return &DefaultService{
		period:    30,    // Standard 30-second window
		digits:    6,     // Standard 6-digit codes
		algorithm: "SHA1", // Standard algorithm for compatibility
	}
}

// NewServiceWithConfig creates a TOTP service with custom configuration.
func NewServiceWithConfig(period, digits uint, algorithm string) *DefaultService {
	return &DefaultService{
		period:    period,
		digits:    digits,
		algorithm: algorithm,
	}
}

// GenerateCode generates a TOTP code from a secret string.
func (s *DefaultService) GenerateCode(secret string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("totp: secret cannot be empty")
	}

	// Generate code for current time
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		return "", fmt.Errorf("totp: failed to generate code: %w", err)
	}

	return code, nil
}

// ValidateCode validates a TOTP code against a secret.
func (s *DefaultService) ValidateCode(secret, code string) bool {
	if secret == "" || code == "" {
		return false
	}

	// Validate with a reasonable time skew (1 period before/after)
	return totp.Validate(code, secret)
}

// GenerateCodeAtTime generates a TOTP code for a specific time.
func (s *DefaultService) GenerateCodeAtTime(secret string, t time.Time) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("totp: secret cannot be empty")
	}

	code, err := totp.GenerateCode(secret, t)
	if err != nil {
		return "", fmt.Errorf("totp: failed to generate code at time %v: %w", t, err)
	}

	return code, nil
}

// GetTimeWindow returns the current TOTP time window information.
func (s *DefaultService) GetTimeWindow() (current int64, remaining time.Duration) {
	now := time.Now()
	current = now.Unix() / int64(s.period)
	
	// Calculate time remaining in current window
	nextWindow := (current + 1) * int64(s.period)
	remaining = time.Unix(nextWindow, 0).Sub(now)
	
	return current, remaining
}

// IsValidSecret checks if a secret string is valid for TOTP.
func (s *DefaultService) IsValidSecret(secret string) error {
	if secret == "" {
		return fmt.Errorf("totp: secret cannot be empty")
	}

	// Try to generate a code to validate the secret
	_, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		return fmt.Errorf("totp: invalid secret format: %w", err)
	}

	return nil
}