package models

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// Vault represents a synchronized vault.
type Vault struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	EncryptionInfo KeyInfo   `json:"encryption_info"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// KeyInfo contains encryption parameters for a vault.
type KeyInfo struct {
	Version           int    `json:"version"`
	EncryptionVersion int    `json:"encryption_version"`
	Salt              string `json:"salt"` // Base64 encoded
}

// Validate validates the vault structure and data.
func (v *Vault) Validate() error {
	if strings.TrimSpace(v.ID) == "" {
		return fmt.Errorf("vault ID is required")
	}
	
	if strings.TrimSpace(v.Name) == "" {
		return fmt.Errorf("vault name is required")
	}
	
	if err := v.EncryptionInfo.Validate(); err != nil {
		return fmt.Errorf("encryption info validation failed: %w", err)
	}
	
	if v.CreatedAt.IsZero() {
		return fmt.Errorf("created_at timestamp is required")
	}
	
	if v.UpdatedAt.IsZero() {
		return fmt.Errorf("updated_at timestamp is required")
	}
	
	if v.UpdatedAt.Before(v.CreatedAt) {
		return fmt.Errorf("updated_at cannot be before created_at")
	}
	
	return nil
}

// Validate validates the key info structure.
func (k *KeyInfo) Validate() error {
	if k.Version < 1 {
		return fmt.Errorf("key info version must be >= 1")
	}
	
	// Validate supported encryption versions
	switch k.EncryptionVersion {
	case 1, 2, 3:
		// Supported versions
	default:
		return fmt.Errorf("unsupported encryption version: %d", k.EncryptionVersion)
	}
	
	if strings.TrimSpace(k.Salt) == "" {
		return fmt.Errorf("salt is required")
	}
	
	// Validate salt is proper base64
	if _, err := base64.StdEncoding.DecodeString(k.Salt); err != nil {
		return fmt.Errorf("invalid salt encoding: must be valid base64")
	}
	
	return nil
}