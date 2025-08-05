package models

import (
	"fmt"
	"strings"
	"time"
)

// SyncState tracks the synchronization progress for a vault.
type SyncState struct {
	VaultID      string            `json:"vault_id"`
	Version      int               `json:"version"`      // Last synced UID
	Files        map[string]string `json:"files"`        // Path -> Hash
	LastSyncTime time.Time         `json:"last_sync_time"`
	LastError    string            `json:"last_error,omitempty"`
}

// NewSyncState creates an empty sync state.
func NewSyncState(vaultID string) *SyncState {
	return &SyncState{
		VaultID: vaultID,
		Version: 0,
		Files:   make(map[string]string),
	}
}

// UpdateFile adds or updates a file in the sync state.
func (s *SyncState) UpdateFile(path, hash string) {
	if s.Files == nil {
		s.Files = make(map[string]string)
	}
	s.Files[path] = hash
}

// RemoveFile removes a file from the sync state.
func (s *SyncState) RemoveFile(path string) {
	if s.Files != nil {
		delete(s.Files, path)
	}
}

// HasFile checks if a file exists in the sync state.
func (s *SyncState) HasFile(path string) bool {
	if s.Files == nil {
		return false
	}
	_, exists := s.Files[path]
	return exists
}

// GetFileHash returns the hash for a file, or empty string if not found.
func (s *SyncState) GetFileHash(path string) string {
	if s.Files == nil {
		return ""
	}
	return s.Files[path]
}

// FileCount returns the number of files in the sync state.
func (s *SyncState) FileCount() int {
	if s.Files == nil {
		return 0
	}
	return len(s.Files)
}

// UpdateVersion updates the sync version and timestamp.
func (s *SyncState) UpdateVersion(version int) {
	s.Version = version
	s.LastSyncTime = time.Now()
}

// SetError sets the last error message.
func (s *SyncState) SetError(err error) {
	if err != nil {
		s.LastError = err.Error()
	} else {
		s.LastError = ""
	}
}

// ClearError clears the last error message.
func (s *SyncState) ClearError() {
	s.LastError = ""
}

// HasError returns true if there's a stored error.
func (s *SyncState) HasError() bool {
	return strings.TrimSpace(s.LastError) != ""
}

// Validate validates the sync state structure.
func (s *SyncState) Validate() error {
	if strings.TrimSpace(s.VaultID) == "" {
		return fmt.Errorf("vault ID is required")
	}
	
	if s.Version < 0 {
		return fmt.Errorf("version cannot be negative")
	}
	
	if s.Files == nil {
		return fmt.Errorf("files map cannot be nil")
	}
	
	// Validate file paths and hashes
	for path, hash := range s.Files {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("file path cannot be empty")
		}
		
		if strings.TrimSpace(hash) == "" {
			return fmt.Errorf("file hash cannot be empty for path: %s", path)
		}
		
		// Basic hash format validation (hex string)
		if len(hash) < 8 || len(hash) > 128 {
			return fmt.Errorf("file hash has invalid length for path %s: %d", path, len(hash))
		}
	}
	
	return nil
}

// Clone creates a deep copy of the sync state.
func (s *SyncState) Clone() *SyncState {
	clone := &SyncState{
		VaultID:      s.VaultID,
		Version:      s.Version,
		LastSyncTime: s.LastSyncTime,
		LastError:    s.LastError,
		Files:        make(map[string]string),
	}
	
	for path, hash := range s.Files {
		clone.Files[path] = hash
	}
	
	return clone
}