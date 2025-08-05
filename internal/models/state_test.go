package models_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/yourusername/obsync/internal/models"
)

func TestNewSyncStateBasic(t *testing.T) {
	tests := []struct {
		name    string
		vaultID string
		want    *models.SyncState
	}{
		{
			name:    "creates empty state",
			vaultID: "vault-123",
			want: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files:   map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := models.NewSyncState(tt.vaultID)

			assert.Equal(t, tt.want.VaultID, got.VaultID)
			assert.Equal(t, tt.want.Version, got.Version)
			assert.NotNil(t, got.Files)
			assert.Empty(t, got.Files)
		})
	}
}

func TestSyncError_Error(t *testing.T) {
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
				Err:     models.ErrDecryptionFailed,
			},
			want: "sync decrypt [DECRYPTION_ERROR]: vault vault-123: notes/test.md: decryption failed",
		},
		{
			name: "without path",
			err: &models.SyncError{
				Code:    models.ErrCodeAuth,
				Phase:   "connect",
				VaultID: "vault-123",
				Err:     models.ErrNotAuthenticated,
			},
			want: "sync connect [AUTH_ERROR]: vault vault-123: not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.err.Error())
		})
	}
}