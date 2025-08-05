package models_test

import (
	"errors"
	"testing"
	
	"github.com/stretchr/testify/assert"
	
	"github.com/TheMichaelB/obsync/internal/models"
)

func TestSyncError(t *testing.T) {
	tests := []struct {
		name string
		err  *models.SyncError
		want string
	}{
		{
			name: "with path",
			err: &models.SyncError{
				Code:    models.ErrCodeDecryption,
				Phase:   "decrypt",
				VaultID: "vault-123",
				Path:    "notes/test.md",
				Err:     errors.New("key derivation failed"),
			},
			want: "sync decrypt [DECRYPTION_ERROR]: vault vault-123: notes/test.md: key derivation failed",
		},
		{
			name: "without path",
			err: &models.SyncError{
				Code:    models.ErrCodeNetwork,
				Phase:   "transport",
				VaultID: "vault-456",
				Err:     errors.New("connection timeout"),
			},
			want: "sync transport [NETWORK_ERROR]: vault vault-456: connection timeout",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAPIError(t *testing.T) {
	err := &models.APIError{
		Code:       "UNAUTHORIZED",
		Message:    "Invalid token",
		StatusCode: 401,
		RequestID:  "req-123",
	}
	
	want := "API error 401 (UNAUTHORIZED): Invalid token"
	assert.Equal(t, want, err.Error())
}

func TestDecryptError(t *testing.T) {
	tests := []struct {
		name string
		err  *models.DecryptError
		want string
	}{
		{
			name: "with path",
			err: &models.DecryptError{
				Path:   "notes/secret.md",
				Reason: "invalid key",
				Err:    errors.New("cipher: message authentication failed"),
			},
			want: "decrypt notes/secret.md: invalid key: cipher: message authentication failed",
		},
		{
			name: "without path",
			err: &models.DecryptError{
				Reason: "key derivation",
				Err:    errors.New("pbkdf2 failed"),
			},
			want: "decrypt: key derivation: pbkdf2 failed",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIntegrityError(t *testing.T) {
	err := &models.IntegrityError{
		Path:     "notes/test.md",
		Expected: "abc123",
		Actual:   "def456",
	}
	
	want := "integrity check failed for notes/test.md: expected abc123, got def456"
	assert.Equal(t, want, err.Error())
}

func TestErrorUnwrapping(t *testing.T) {
	baseErr := errors.New("base error")
	
	t.Run("SyncError unwrap", func(t *testing.T) {
		syncErr := &models.SyncError{
			Code:    models.ErrCodeNetwork,
			Phase:   "connect",
			VaultID: "vault-123",
			Err:     baseErr,
		}
		
		assert.Equal(t, baseErr, errors.Unwrap(syncErr))
	})
	
	t.Run("DecryptError unwrap", func(t *testing.T) {
		decryptErr := &models.DecryptError{
			Path:   "test.md",
			Reason: "invalid key",
			Err:    baseErr,
		}
		
		assert.Equal(t, baseErr, errors.Unwrap(decryptErr))
	})
}