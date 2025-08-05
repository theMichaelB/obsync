package models

import (
	"errors"
	"fmt"
)

// Error codes for structured error handling.
const (
	ErrCodeAuth           = "AUTH_ERROR"
	ErrCodeVaultNotFound  = "VAULT_NOT_FOUND"
	ErrCodeDecryption     = "DECRYPTION_ERROR"
	ErrCodeIntegrity      = "INTEGRITY_ERROR"
	ErrCodeNetwork        = "NETWORK_ERROR"
	ErrCodeStorage        = "STORAGE_ERROR"
	ErrCodeState          = "STATE_ERROR"
	ErrCodeConfig         = "CONFIG_ERROR"
	ErrCodeRateLimit      = "RATE_LIMIT"
	ErrCodeServerError    = "SERVER_ERROR"
)

// Sentinel errors
var (
	ErrNotAuthenticated   = errors.New("not authenticated")
	ErrVaultNotFound      = errors.New("vault not found")
	ErrSyncInProgress     = errors.New("sync already in progress")
	ErrInvalidConfig      = errors.New("invalid configuration")
	ErrDecryptionFailed   = errors.New("decryption failed")
	ErrIntegrityCheckFail = errors.New("integrity check failed")
	ErrRateLimited        = errors.New("rate limited")
	ErrConnectionLost     = errors.New("connection lost")
	ErrSyncComplete       = errors.New("sync completed successfully")
)

// APIError represents an error from the API.
type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	StatusCode int    `json:"status_code"`
	RequestID  string `json:"request_id,omitempty"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d (%s): %s", e.StatusCode, e.Code, e.Message)
}

// SyncError provides detailed sync failure information.
type SyncError struct {
	Code    string
	Phase   string
	VaultID string
	Path    string
	Err     error
}

func (e *SyncError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("sync %s [%s]: vault %s: %s: %v", e.Phase, e.Code, e.VaultID, e.Path, e.Err)
	}
	return fmt.Sprintf("sync %s [%s]: vault %s: %v", e.Phase, e.Code, e.VaultID, e.Err)
}

func (e *SyncError) Unwrap() error {
	return e.Err
}

// DecryptError represents a decryption failure.
type DecryptError struct {
	Path   string
	Reason string
	Err    error
}

func (e *DecryptError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("decrypt %s: %s: %v", e.Path, e.Reason, e.Err)
	}
	return fmt.Sprintf("decrypt: %s: %v", e.Reason, e.Err)
}

func (e *DecryptError) Unwrap() error {
	return e.Err
}

// IntegrityError represents a hash mismatch.
type IntegrityError struct {
	Path     string
	Expected string
	Actual   string
}

func (e *IntegrityError) Error() string {
	return fmt.Sprintf("integrity check failed for %s: expected %s, got %s", 
		e.Path, e.Expected, e.Actual)
}