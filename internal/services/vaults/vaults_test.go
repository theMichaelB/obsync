package vaults_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TheMichaelB/obsync/internal/crypto"
	"github.com/TheMichaelB/obsync/internal/services/vaults"
	"github.com/TheMichaelB/obsync/internal/transport"
	"github.com/TheMichaelB/obsync/test/testutil"
)

func TestVaultService(t *testing.T) {
	logger := testutil.NewTestLogger()
	mockTransport := transport.NewMockTransport()
	cryptoProvider := crypto.NewProvider()

	service := vaults.NewService(mockTransport, cryptoProvider, logger)

	t.Run("list vaults", func(t *testing.T) {
		// Mock vault list response
		mockTransport.AddResponse("/api/v1/vaults/list", map[string]interface{}{
			"vaults": []interface{}{
				map[string]interface{}{
					"id":   "vault-1",
					"name": "My Vault",
					"encryption_info": map[string]interface{}{
						"version":            float64(1),
						"encryption_version": float64(1),
						"salt":               "dGVzdC1zYWx0", // base64 encoded "test-salt"
					},
				},
				map[string]interface{}{
					"id":   "vault-2",
					"name": "Work Vault",
					"encryption_info": map[string]interface{}{
						"version":            float64(1),
						"encryption_version": float64(1),
						"salt":               "d29yay1zYWx0", // base64 encoded "work-salt"
					},
				},
			},
		})

		vaultList, err := service.ListVaults(context.Background())
		require.NoError(t, err)
		assert.Len(t, vaultList, 2)

		// Check first vault
		vault1 := vaultList[0]
		assert.Equal(t, "vault-1", vault1.ID)
		assert.Equal(t, "My Vault", vault1.Name)
		assert.Equal(t, 1, vault1.EncryptionInfo.Version)
		assert.Equal(t, 1, vault1.EncryptionInfo.EncryptionVersion)
		assert.Equal(t, "dGVzdC1zYWx0", vault1.EncryptionInfo.Salt)

		// Check second vault
		vault2 := vaultList[1]
		assert.Equal(t, "vault-2", vault2.ID)
		assert.Equal(t, "Work Vault", vault2.Name)
	})

	t.Run("get specific vault", func(t *testing.T) {
		// Mock get vault response
		mockTransport.AddResponse("/api/v1/vaults/get", map[string]interface{}{
			"id":   "vault-3",
			"name": "Test Vault",
			"encryption_info": map[string]interface{}{
				"version":            float64(2),
				"encryption_version": float64(2),
				"salt":               "dGVzdC12YXVsdA==", // base64 encoded "test-vault"
			},
		})

		vault, err := service.GetVault(context.Background(), "vault-3")
		require.NoError(t, err)
		assert.Equal(t, "vault-3", vault.ID)
		assert.Equal(t, "Test Vault", vault.Name)
		assert.Equal(t, 2, vault.EncryptionInfo.Version)
		assert.Equal(t, 2, vault.EncryptionInfo.EncryptionVersion)
	})

	t.Run("get vault from cache", func(t *testing.T) {
		// Should return cached vault without making request
		vault, err := service.GetVault(context.Background(), "vault-1") // From previous test
		require.NoError(t, err)
		assert.Equal(t, "vault-1", vault.ID)
		assert.Equal(t, "My Vault", vault.Name)
	})

	t.Run("derive vault key", func(t *testing.T) {
		// Clear cache to avoid conflicts with previous tests
		service.ClearCache()
		
		// Mock vault response for key derivation
		mockTransport.AddResponse("/api/v1/vaults/get", map[string]interface{}{
			"id":   "vault-key-test",
			"name": "Key Test Vault",
			"encryption_info": map[string]interface{}{
				"version":            float64(1),
				"encryption_version": float64(3), // Should match crypto.EncryptionVersion
				"salt":               "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWYwMTIzNDU2Nzg5YWJjZGVmMDEyMzQ1Njc4OWFiY2RlZg==", // base64 encoded 32-byte salt
			},
		})

		key, err := service.GetVaultKey(context.Background(), "vault-key-test", "user@example.com", "password")
		require.NoError(t, err)
		assert.NotEmpty(t, key)
		assert.Len(t, key, 32) // 256-bit key
	})

	t.Run("cached vault key", func(t *testing.T) {
		// Should return cached key
		key2, err := service.GetVaultKey(context.Background(), "vault-key-test", "user@example.com", "password")
		require.NoError(t, err)
		assert.NotEmpty(t, key2)

		// Should be same as first call (cached)
		key1, _ := service.GetVaultKey(context.Background(), "vault-key-test", "user@example.com", "password")
		assert.Equal(t, key1, key2)
	})

	t.Run("clear cache", func(t *testing.T) {
		service.ClearCache()

		// Mock response again since cache was cleared
		mockTransport.AddResponse("/api/v1/vaults/get", map[string]interface{}{
			"id":   "vault-1",
			"name": "My Vault",
			"encryption_info": map[string]interface{}{
				"version":            float64(1),
				"encryption_version": float64(1),
				"salt":               "dGVzdC1zYWx0",
			},
		})

		// Should make new request since cache was cleared
		vault, err := service.GetVault(context.Background(), "vault-1")
		require.NoError(t, err)
		assert.Equal(t, "vault-1", vault.ID)
	})
}

func TestVaultServiceErrors(t *testing.T) {
	logger := testutil.NewTestLogger()
	mockTransport := transport.NewMockTransport()
	cryptoProvider := crypto.NewProvider()

	service := vaults.NewService(mockTransport, cryptoProvider, logger)

	t.Run("invalid vault list response", func(t *testing.T) {
		mockTransport.AddResponse("/api/v1/vaults/list", map[string]interface{}{
			"invalid": "response",
		})

		_, err := service.ListVaults(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid vault list response")
	})

	t.Run("vault not found", func(t *testing.T) {
		mockTransport.AddError("/api/v1/vaults/get", fmt.Errorf("Vault not found"))

		_, err := service.GetVault(context.Background(), "nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "get vault")
	})

	t.Run("invalid salt encoding", func(t *testing.T) {
		// Create fresh transport and service for this test
		freshTransport := transport.NewMockTransport()
		freshService := vaults.NewService(freshTransport, cryptoProvider, logger)
		
		freshTransport.AddResponse("/api/v1/vaults/get", map[string]interface{}{
			"id":   "vault-bad-salt",
			"name": "Bad Salt Vault",
			"encryption_info": map[string]interface{}{
				"version":            float64(1),
				"encryption_version": float64(3), // Valid encryption version
				"salt":               "invalid-base64!", // Invalid base64
			},
		})

		_, err := freshService.GetVaultKey(context.Background(), "vault-bad-salt", "user@example.com", "password")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decode salt")
	})
}