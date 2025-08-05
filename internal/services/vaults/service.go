package vaults

import (
	"context"
	"fmt"

	"github.com/yourusername/obsync/internal/crypto"
	"github.com/yourusername/obsync/internal/events"
	"github.com/yourusername/obsync/internal/models"
	"github.com/yourusername/obsync/internal/transport"
)

// Service manages vault operations.
type Service struct {
	transport transport.Transport
	crypto    crypto.Provider
	logger    *events.Logger

	// Cache
	vaults map[string]*models.Vault
	keys   map[string][]byte
}

// NewService creates a vault service.
func NewService(transport transport.Transport, crypto crypto.Provider, logger *events.Logger) *Service {
	return &Service{
		transport: transport,
		crypto:    crypto,
		logger:    logger.WithField("service", "vaults"),
		vaults:    make(map[string]*models.Vault),
		keys:      make(map[string][]byte),
	}
}

// ListVaults fetches available vaults.
func (s *Service) ListVaults(ctx context.Context) ([]*models.Vault, error) {
	s.logger.Debug("Fetching vault list")

	resp, err := s.transport.PostJSON(ctx, "/vault/list", map[string]interface{}{
		"token": s.transport.GetToken(),
		"supported_encryption_version": 3,
	})
	if err != nil {
		return nil, fmt.Errorf("list vaults: %w", err)
	}

	// Parse vaults array
	vaultsData, ok := resp["vaults"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid vault list response")
	}

	var vaults []*models.Vault
	for _, v := range vaultsData {
		vaultMap, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		vault := &models.Vault{
			ID:   getString(vaultMap, "id"),
			Name: getString(vaultMap, "name"),
			Host: getString(vaultMap, "host"),
		}

		// Parse encryption info (fields are directly in vault data)
		vault.EncryptionInfo = models.KeyInfo{
			Version:           1, // Default version
			EncryptionVersion: getInt(vaultMap, "encryption_version"),
			Salt:              getString(vaultMap, "salt"),
		}

		vaults = append(vaults, vault)
		s.vaults[vault.ID] = vault // Cache
	}

	s.logger.WithField("count", len(vaults)).Info("Fetched vaults")
	return vaults, nil
}

// GetVault retrieves a specific vault.
func (s *Service) GetVault(ctx context.Context, vaultID string) (*models.Vault, error) {
	// Check cache
	if vault, ok := s.vaults[vaultID]; ok {
		return vault, nil
	}

	// If not in cache, fetch vault list to populate cache
	_, err := s.ListVaults(ctx)
	if err != nil {
		return nil, fmt.Errorf("get vault: %w", err)
	}

	// Check cache again after listing vaults
	if vault, ok := s.vaults[vaultID]; ok {
		return vault, nil
	}

	return nil, fmt.Errorf("vault not found: %s", vaultID)
}

// GetVaultKey derives the encryption key for a vault.
func (s *Service) GetVaultKey(ctx context.Context, vaultID, email, password string) ([]byte, error) {
	// Check cache
	if key, ok := s.keys[vaultID]; ok {
		return key, nil
	}

	// Get vault info
	vault, err := s.GetVault(ctx, vaultID)
	if err != nil {
		return nil, err
	}

	s.logger.WithFields(map[string]interface{}{
		"vault_id":           vaultID,
		"encryption_version": vault.EncryptionInfo.EncryptionVersion,
	}).Debug("Deriving vault key")

	// Derive key
	keyInfo := crypto.VaultKeyInfo{
		Version:           vault.EncryptionInfo.Version,
		EncryptionVersion: vault.EncryptionInfo.EncryptionVersion,
		Salt:              vault.EncryptionInfo.Salt, // Pass salt as-is, crypto provider handles encoding
	}

	key, err := s.crypto.DeriveKey(email, password, keyInfo)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	// Cache key
	s.keys[vaultID] = key

	return key, nil
}

// ClearCache removes cached vaults and keys.
func (s *Service) ClearCache() {
	s.vaults = make(map[string]*models.Vault)
	s.keys = make(map[string][]byte)
}

// Helper functions

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}