package state

import (
	"errors"
	"time"

	"github.com/yourusername/obsync/internal/models"
)

// Store manages sync state persistence.
type Store interface {
	// Load retrieves the sync state for a vault.
	Load(vaultID string) (*models.SyncState, error)

	// Save persists the sync state for a vault.
	Save(vaultID string, state *models.SyncState) error

	// Reset removes all state for a vault.
	Reset(vaultID string) error

	// List returns all known vault IDs.
	List() ([]string, error)

	// Lock acquires an exclusive lock for a vault.
	Lock(vaultID string) (UnlockFunc, error)

	// Migrate transfers state between stores.
	Migrate(target Store) error

	// Close releases resources.
	Close() error
}

// UnlockFunc releases a vault lock.
type UnlockFunc func()

// Errors
var (
	ErrStateNotFound = errors.New("state not found")
	ErrStateLocked   = errors.New("state is locked")
	ErrStateCorrupt  = errors.New("state file is corrupt")
)

// SyncState extends the model with store metadata.
type SyncState struct {
	*models.SyncState

	// Store metadata
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	Checksum      string    `json:"checksum,omitempty"`
}

// CurrentSchemaVersion for migrations.
const CurrentSchemaVersion = 1

// SyncStateInfo provides summary information about a vault's sync state.
type SyncStateInfo struct {
	VaultID      string            `json:"vault_id"`
	Version      int               `json:"version"`
	Files        map[string]string `json:"files"`
	LastSyncTime time.Time         `json:"last_sync_time"`
	LastError    string            `json:"last_error,omitempty"`
}