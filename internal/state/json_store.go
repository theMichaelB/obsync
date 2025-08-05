package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yourusername/obsync/internal/events"
	"github.com/yourusername/obsync/internal/models"
)

// JSONStore implements file-based state storage.
type JSONStore struct {
	baseDir string
	logger  *events.Logger

	// Locking
	mu    sync.RWMutex
	locks map[string]*sync.Mutex
}

// NewJSONStore creates a JSON-based state store.
func NewJSONStore(baseDir string, logger *events.Logger) (*JSONStore, error) {
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}

	return &JSONStore{
		baseDir: baseDir,
		logger:  logger.WithField("component", "json_state_store"),
		locks:   make(map[string]*sync.Mutex),
	}, nil
}

// Load reads state from JSON file.
func (s *JSONStore) Load(vaultID string) (*models.SyncState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.statePath(vaultID)

	s.logger.WithFields(map[string]interface{}{
		"vault_id": vaultID,
		"path":     path,
	}).Debug("Loading state")

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, ErrStateNotFound
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}

	// Verify checksum
	var wrapper SyncState
	if err := json.Unmarshal(data, &wrapper); err != nil {
		// Try backup file
		if state, err := s.loadBackup(vaultID); err == nil {
			s.logger.Warn("Loaded state from backup due to corruption")
			return state, nil
		}
		return nil, ErrStateCorrupt
	}

	// Verify checksum if present
	if wrapper.Checksum != "" {
		// Create copy without checksum for verification
		verification := SyncState{
			SyncState:     wrapper.SyncState,
			SchemaVersion: wrapper.SchemaVersion,
			CreatedAt:     wrapper.CreatedAt,
		}
		verifyData, _ := json.Marshal(verification)
		hash := sha256.Sum256(verifyData)
		calculated := hex.EncodeToString(hash[:])

		if calculated != wrapper.Checksum {
			s.logger.WithFields(map[string]interface{}{
				"expected": wrapper.Checksum,
				"actual":   calculated,
			}).Error("State checksum mismatch")

			// Try backup
			if state, err := s.loadBackup(vaultID); err == nil {
				return state, nil
			}
			return nil, ErrStateCorrupt
		}
	}

	// Check schema version
	if wrapper.SchemaVersion != CurrentSchemaVersion {
		s.logger.WithField("version", wrapper.SchemaVersion).Warn("State schema version mismatch")
		// In production, handle migration here
	}

	return wrapper.SyncState, nil
}

// Save writes state to JSON file.
func (s *JSONStore) Save(vaultID string, state *models.SyncState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.statePath(vaultID)

	s.logger.WithFields(map[string]interface{}{
		"vault_id": vaultID,
		"version":  state.Version,
		"files":    len(state.Files),
	}).Debug("Saving state")

	// Create wrapper with metadata
	wrapper := SyncState{
		SyncState:     state,
		SchemaVersion: CurrentSchemaVersion,
		CreatedAt:     time.Now(),
	}

	// Calculate checksum (without checksum field)
	checksumWrapper := SyncState{
		SyncState:     wrapper.SyncState,
		SchemaVersion: wrapper.SchemaVersion,
		CreatedAt:     wrapper.CreatedAt,
		// Checksum field left empty for calculation
	}
	checksumData, err := json.Marshal(checksumWrapper)
	if err != nil {
		return fmt.Errorf("marshal state for checksum: %w", err)
	}

	hash := sha256.Sum256(checksumData)
	wrapper.Checksum = hex.EncodeToString(hash[:])

	// Marshal final version with checksum
	jsonData, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state with checksum: %w", err)
	}

	// Create backup of existing file
	if _, err := os.Stat(path); err == nil {
		backupPath := path + ".backup"
		if err := s.copyFile(path, backupPath); err != nil {
			s.logger.WithError(err).Warn("Failed to create backup")
		}
	}

	// Write atomically
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, jsonData, 0600); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	// Sync to disk
	if file, err := os.Open(tmpPath); err == nil {
		_ = file.Sync()
		file.Close()
	}

	// Rename atomically
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename state file: %w", err)
	}

	return nil
}

// Reset removes state for a vault.
func (s *JSONStore) Reset(vaultID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithField("vault_id", vaultID).Info("Resetting state")

	path := s.statePath(vaultID)
	backupPath := path + ".backup"

	// Remove files
	_ = os.Remove(path)
	_ = os.Remove(backupPath)

	return nil
}

// List returns all vault IDs with state.
func (s *JSONStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("read state directory: %w", err)
	}

	var vaultIDs []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if filepath.Ext(name) == ".json" && !strings.HasSuffix(name, ".backup.json") {
			vaultID := strings.TrimSuffix(name, ".json")
			vaultIDs = append(vaultIDs, vaultID)
		}
	}

	return vaultIDs, nil
}

// Lock acquires a lock for a vault.
func (s *JSONStore) Lock(vaultID string) (UnlockFunc, error) {
	s.mu.Lock()
	lock, exists := s.locks[vaultID]
	if !exists {
		lock = &sync.Mutex{}
		s.locks[vaultID] = lock
	}
	s.mu.Unlock()

	// Try to acquire lock with timeout
	done := make(chan struct{})
	go func() {
		lock.Lock()
		close(done)
	}()

	select {
	case <-done:
		return func() { lock.Unlock() }, nil
	case <-time.After(5 * time.Second):
		return nil, ErrStateLocked
	}
}

// Migrate transfers all states to another store.
func (s *JSONStore) Migrate(target Store) error {
	vaultIDs, err := s.List()
	if err != nil {
		return fmt.Errorf("list vaults: %w", err)
	}

	s.logger.WithField("count", len(vaultIDs)).Info("Migrating states")

	for _, vaultID := range vaultIDs {
		state, err := s.Load(vaultID)
		if err != nil {
			s.logger.WithError(err).WithField("vault_id", vaultID).Error("Failed to load state")
			continue
		}

		if err := target.Save(vaultID, state); err != nil {
			return fmt.Errorf("save vault %s: %w", vaultID, err)
		}

		s.logger.WithField("vault_id", vaultID).Debug("Migrated state")
	}

	return nil
}

// Close releases resources.
func (s *JSONStore) Close() error {
	return nil
}

// Helper methods

func (s *JSONStore) statePath(vaultID string) string {
	return filepath.Join(s.baseDir, vaultID+".json")
}

func (s *JSONStore) loadBackup(vaultID string) (*models.SyncState, error) {
	backupPath := s.statePath(vaultID) + ".backup"

	data, err := os.ReadFile(backupPath)
	if err != nil {
		return nil, err
	}

	var wrapper SyncState
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}

	return wrapper.SyncState, nil
}

func (s *JSONStore) copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}