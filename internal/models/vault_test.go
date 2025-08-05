package models_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/obsync/internal/models"
)

func TestVault_Validate(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	validKeyInfo := models.KeyInfo{
		Version:           1,
		EncryptionVersion: 3,
		Salt:              "dGVzdHNhbHQ=", // "testsalt" in base64
	}

	tests := []struct {
		name    string
		vault   models.Vault
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid vault",
			vault: models.Vault{
				ID:             "vault-123",
				Name:           "Test Vault",
				EncryptionInfo: validKeyInfo,
				CreatedAt:      baseTime,
				UpdatedAt:      baseTime.Add(time.Hour),
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			vault: models.Vault{
				Name:           "Test Vault",
				EncryptionInfo: validKeyInfo,
				CreatedAt:      baseTime,
				UpdatedAt:      baseTime.Add(time.Hour),
			},
			wantErr: true,
			errMsg:  "vault ID is required",
		},
		{
			name: "empty ID after trim",
			vault: models.Vault{
				ID:             "   ",
				Name:           "Test Vault",
				EncryptionInfo: validKeyInfo,
				CreatedAt:      baseTime,
				UpdatedAt:      baseTime.Add(time.Hour),
			},
			wantErr: true,
			errMsg:  "vault ID is required",
		},
		{
			name: "missing name",
			vault: models.Vault{
				ID:             "vault-123",
				EncryptionInfo: validKeyInfo,
				CreatedAt:      baseTime,
				UpdatedAt:      baseTime.Add(time.Hour),
			},
			wantErr: true,
			errMsg:  "vault name is required",
		},
		{
			name: "empty name after trim",
			vault: models.Vault{
				ID:             "vault-123",
				Name:           "  \t  ",
				EncryptionInfo: validKeyInfo,
				CreatedAt:      baseTime,
				UpdatedAt:      baseTime.Add(time.Hour),
			},
			wantErr: true,
			errMsg:  "vault name is required",
		},
		{
			name: "invalid encryption info",
			vault: models.Vault{
				ID:   "vault-123",
				Name: "Test Vault",
				EncryptionInfo: models.KeyInfo{
					Version:           0, // Invalid
					EncryptionVersion: 3,
					Salt:              "dGVzdHNhbHQ=",
				},
				CreatedAt: baseTime,
				UpdatedAt: baseTime.Add(time.Hour),
			},
			wantErr: true,
			errMsg:  "encryption info validation failed",
		},
		{
			name: "zero created_at",
			vault: models.Vault{
				ID:             "vault-123",
				Name:           "Test Vault",
				EncryptionInfo: validKeyInfo,
				UpdatedAt:      baseTime.Add(time.Hour),
			},
			wantErr: true,
			errMsg:  "created_at timestamp is required",
		},
		{
			name: "zero updated_at",
			vault: models.Vault{
				ID:             "vault-123",
				Name:           "Test Vault",
				EncryptionInfo: validKeyInfo,
				CreatedAt:      baseTime,
			},
			wantErr: true,
			errMsg:  "updated_at timestamp is required",
		},
		{
			name: "updated_at before created_at",
			vault: models.Vault{
				ID:             "vault-123",
				Name:           "Test Vault",
				EncryptionInfo: validKeyInfo,
				CreatedAt:      baseTime.Add(time.Hour),
				UpdatedAt:      baseTime, // Before created_at
			},
			wantErr: true,
			errMsg:  "updated_at cannot be before created_at",
		},
		{
			name: "updated_at same as created_at",
			vault: models.Vault{
				ID:             "vault-123",
				Name:           "Test Vault",
				EncryptionInfo: validKeyInfo,
				CreatedAt:      baseTime,
				UpdatedAt:      baseTime, // Same time is valid
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.vault.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKeyInfo_Validate(t *testing.T) {
	tests := []struct {
		name    string
		keyInfo models.KeyInfo
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid key info v1",
			keyInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 1,
				Salt:              "dGVzdHNhbHQ=",
			},
			wantErr: false,
		},
		{
			name: "valid key info v2",
			keyInfo: models.KeyInfo{
				Version:           2,
				EncryptionVersion: 2,
				Salt:              "YWRkaXRpb25hbHRlc3RzYWx0",
			},
			wantErr: false,
		},
		{
			name: "valid key info v3",
			keyInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 3,
				Salt:              "bG9uZ2VydGVzdHNhbHR2YWx1ZXM=",
			},
			wantErr: false,
		},
		{
			name: "zero version",
			keyInfo: models.KeyInfo{
				Version:           0,
				EncryptionVersion: 3,
				Salt:              "dGVzdHNhbHQ=",
			},
			wantErr: true,
			errMsg:  "key info version must be >= 1",
		},
		{
			name: "negative version",
			keyInfo: models.KeyInfo{
				Version:           -1,
				EncryptionVersion: 3,
				Salt:              "dGVzdHNhbHQ=",
			},
			wantErr: true,
			errMsg:  "key info version must be >= 1",
		},
		{
			name: "unsupported encryption version 0",
			keyInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 0,
				Salt:              "dGVzdHNhbHQ=",
			},
			wantErr: true,
			errMsg:  "unsupported encryption version: 0",
		},
		{
			name: "unsupported encryption version 4",
			keyInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 4,
				Salt:              "dGVzdHNhbHQ=",
			},
			wantErr: true,
			errMsg:  "unsupported encryption version: 4",
		},
		{
			name: "unsupported encryption version 999",
			keyInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 999,
				Salt:              "dGVzdHNhbHQ=",
			},
			wantErr: true,
			errMsg:  "unsupported encryption version: 999",
		},
		{
			name: "empty salt",
			keyInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 3,
				Salt:              "",
			},
			wantErr: true,
			errMsg:  "salt is required",
		},
		{
			name: "whitespace salt",
			keyInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 3,
				Salt:              "   \t  ",
			},
			wantErr: true,
			errMsg:  "salt is required",
		},
		{
			name: "invalid base64 salt",
			keyInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 3,
				Salt:              "not-base64!",
			},
			wantErr: true,
			errMsg:  "invalid salt encoding: must be valid base64",
		},
		{
			name: "incomplete base64 salt",
			keyInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 3,
				Salt:              "dGVzdA", // Missing padding
			},
			wantErr: true,
			errMsg:  "invalid salt encoding: must be valid base64",
		},
		{
			name: "valid base64 with padding",
			keyInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 3,
				Salt:              "dGVzdA==", // Proper padding
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.keyInfo.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVault_JSON_Marshaling(t *testing.T) {
	baseTime := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)
	vault := models.Vault{
		ID:   "vault-test-123",
		Name: "Test Vault for JSON",
		EncryptionInfo: models.KeyInfo{
			Version:           2,
			EncryptionVersion: 3,
			Salt:              "dGVzdHNhbHRmb3Jqc29u",
		},
		CreatedAt: baseTime,
		UpdatedAt: baseTime.Add(2 * time.Hour),
	}

	// Test JSON marshaling
	data, err := json.Marshal(vault)
	require.NoError(t, err)
	assert.Contains(t, string(data), "vault-test-123")
	assert.Contains(t, string(data), "Test Vault for JSON")

	// Test JSON unmarshaling
	var unmarshaled models.Vault
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, vault.ID, unmarshaled.ID)
	assert.Equal(t, vault.Name, unmarshaled.Name)
	assert.Equal(t, vault.EncryptionInfo, unmarshaled.EncryptionInfo)
	assert.True(t, vault.CreatedAt.Equal(unmarshaled.CreatedAt))
	assert.True(t, vault.UpdatedAt.Equal(unmarshaled.UpdatedAt))

	// Validate the unmarshaled vault
	err = unmarshaled.Validate()
	assert.NoError(t, err)
}

func TestKeyInfo_JSON_Marshaling(t *testing.T) {
	keyInfo := models.KeyInfo{
		Version:           1,
		EncryptionVersion: 3,
		Salt:              "dGVzdGtleWluZm9zYWx0",
	}

	// Test JSON marshaling
	data, err := json.Marshal(keyInfo)
	require.NoError(t, err)
	assert.Contains(t, string(data), "dGVzdGtleWluZm9zYWx0")

	// Test JSON unmarshaling
	var unmarshaled models.KeyInfo
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, keyInfo, unmarshaled)

	// Validate the unmarshaled key info
	err = unmarshaled.Validate()
	assert.NoError(t, err)
}

func TestVault_EdgeCases(t *testing.T) {
	t.Run("very long vault ID", func(t *testing.T) {
		longID := strings.Repeat("a", 1000)
		vault := models.Vault{
			ID:   longID,
			Name: "Test",
			EncryptionInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 3,
				Salt:              "dGVzdA==",
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		err := vault.Validate()
		assert.NoError(t, err) // Long IDs should be valid
	})

	t.Run("very long vault name", func(t *testing.T) {
		longName := strings.Repeat("Test ", 200)
		vault := models.Vault{
			ID:   "vault-123",
			Name: longName,
			EncryptionInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 3,
				Salt:              "dGVzdA==",
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		err := vault.Validate()
		assert.NoError(t, err) // Long names should be valid
	})

	t.Run("future timestamps", func(t *testing.T) {
		futureTime := time.Now().Add(24 * time.Hour)
		vault := models.Vault{
			ID:   "vault-123",
			Name: "Test",
			EncryptionInfo: models.KeyInfo{
				Version:           1,
				EncryptionVersion: 3,
				Salt:              "dGVzdA==",
			},
			CreatedAt: futureTime,
			UpdatedAt: futureTime.Add(time.Hour),
		}

		err := vault.Validate()
		assert.NoError(t, err) // Future timestamps should be valid
	})

	t.Run("very large base64 salt", func(t *testing.T) {
		largeSalt := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("salt"), 256))
		keyInfo := models.KeyInfo{
			Version:           1,
			EncryptionVersion: 3,
			Salt:              largeSalt,
		}

		err := keyInfo.Validate()
		assert.NoError(t, err) // Large salts should be valid
	})
}