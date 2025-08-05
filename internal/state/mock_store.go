package state

import (
	"sync"

	"github.com/yourusername/obsync/internal/models"
)

// MockStore provides a mock implementation for testing.
type MockStore struct {
	mu     sync.RWMutex
	states map[string]*models.SyncState
}

// NewMockStore creates a mock state store.
func NewMockStore() *MockStore {
	return &MockStore{
		states: make(map[string]*models.SyncState),
	}
}

// Load loads sync state for a vault.
func (m *MockStore) Load(vaultID string) (*models.SyncState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if state, ok := m.states[vaultID]; ok {
		// Return a copy to avoid race conditions
		copy := *state
		copy.Files = make(map[string]string)
		for k, v := range state.Files {
			copy.Files[k] = v
		}
		return &copy, nil
	}

	return nil, ErrStateNotFound
}

// Save saves sync state for a vault.
func (m *MockStore) Save(vaultID string, state *models.SyncState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store a copy to avoid race conditions
	copy := *state
	copy.Files = make(map[string]string)
	for k, v := range state.Files {
		copy.Files[k] = v
	}
	m.states[vaultID] = &copy
	return nil
}

// Reset removes sync state for a vault.
func (m *MockStore) Reset(vaultID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.states, vaultID)
	return nil
}

// List returns all vault IDs with stored state.
func (m *MockStore) List() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var vaultIDs []string
	for vaultID := range m.states {
		vaultIDs = append(vaultIDs, vaultID)
	}
	return vaultIDs, nil
}

// Helper methods for testing

// SaveState saves state directly (for test setup).
func (m *MockStore) SaveState(vaultID string, state *models.SyncState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[vaultID] = state
}

// Lock acquires an exclusive lock for a vault (no-op for mock).
func (m *MockStore) Lock(vaultID string) (UnlockFunc, error) {
	return func() {}, nil
}

// Migrate transfers state between stores (no-op for mock).
func (m *MockStore) Migrate(target Store) error {
	return nil
}

// Close closes the store (no-op for mock).
func (m *MockStore) Close() error {
	return nil
}

// Clear removes all states.
func (m *MockStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states = make(map[string]*models.SyncState)
}