package state_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/models"
	"github.com/TheMichaelB/obsync/internal/state"
)

func TestJSONStore(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	store, err := state.NewJSONStore(tmpDir, logger)
	require.NoError(t, err)
	defer store.Close()

	testStoreOperations(t, store)
}

func TestSQLiteStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.db")
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	store, err := state.NewSQLiteStore(dbPath, logger)
	require.NoError(t, err)
	defer store.Close()

	testStoreOperations(t, store)
}

func testStoreOperations(t *testing.T, store state.Store) {
	vaultID := "test-vault-123"

	t.Run("load non-existent", func(t *testing.T) {
		_, err := store.Load(vaultID)
		assert.ErrorIs(t, err, state.ErrStateNotFound)
	})

	t.Run("save and load", func(t *testing.T) {
		syncState := &models.SyncState{
			VaultID: vaultID,
			Version: 42,
			Files: map[string]string{
				"notes/test.md":  "hash1",
				"daily/2024.md":  "hash2",
				"folder/sub.txt": "hash3",
			},
			LastSyncTime: time.Now().UTC().Truncate(time.Second),
			LastError:    "",
		}

		err := store.Save(vaultID, syncState)
		require.NoError(t, err)

		loaded, err := store.Load(vaultID)
		require.NoError(t, err)

		assert.Equal(t, syncState.VaultID, loaded.VaultID)
		assert.Equal(t, syncState.Version, loaded.Version)
		assert.Equal(t, syncState.Files, loaded.Files)
		assert.Equal(t, syncState.LastSyncTime.Unix(), loaded.LastSyncTime.Unix())
	})

	t.Run("update existing", func(t *testing.T) {
		// First save
		state1 := &models.SyncState{
			VaultID: vaultID,
			Version: 100,
			Files: map[string]string{
				"file1.md": "hash1",
			},
		}
		err := store.Save(vaultID, state1)
		require.NoError(t, err)

		// Update
		state2 := &models.SyncState{
			VaultID: vaultID,
			Version: 200,
			Files: map[string]string{
				"file1.md": "hash1-updated",
				"file2.md": "hash2",
			},
			LastError: "test error",
		}
		err = store.Save(vaultID, state2)
		require.NoError(t, err)

		// Verify
		loaded, err := store.Load(vaultID)
		require.NoError(t, err)

		assert.Equal(t, 200, loaded.Version)
		assert.Len(t, loaded.Files, 2)
		assert.Equal(t, "hash1-updated", loaded.Files["file1.md"])
		assert.Equal(t, "test error", loaded.LastError)
	})

	t.Run("list vaults", func(t *testing.T) {
		// Save another vault
		err := store.Save("vault-456", &models.SyncState{
			VaultID: "vault-456",
			Version: 1,
			Files:   map[string]string{},
		})
		require.NoError(t, err)

		vaults, err := store.List()
		require.NoError(t, err)

		assert.Contains(t, vaults, vaultID)
		assert.Contains(t, vaults, "vault-456")
		assert.GreaterOrEqual(t, len(vaults), 2)
	})

	t.Run("reset vault", func(t *testing.T) {
		err := store.Reset(vaultID)
		require.NoError(t, err)

		_, err = store.Load(vaultID)
		assert.ErrorIs(t, err, state.ErrStateNotFound)

		// Other vault should still exist
		_, err = store.Load("vault-456")
		assert.NoError(t, err)
	})

	t.Run("concurrent locking", func(t *testing.T) {
		unlock1, err := store.Lock("lock-test")
		require.NoError(t, err)

		// Second lock should timeout or wait
		done := make(chan bool)
		go func() {
			unlock2, err := store.Lock("lock-test")
			if err == nil {
				defer unlock2()
			}
			done <- (err == nil)
		}()

		// Should not complete immediately
		select {
		case success := <-done:
			if success {
				t.Error("Second lock acquired too quickly")
			}
		case <-time.After(100 * time.Millisecond):
			// Expected - lock should be blocked
		}

		// Release first lock
		unlock1()

		// Second lock should now complete
		select {
		case success := <-done:
			if !success {
				t.Error("Second lock failed after first was released")
			}
		case <-time.After(1 * time.Second):
			t.Error("Second lock never acquired")
		}
	})
}

func TestJSONStoreCorruption(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	store, err := state.NewJSONStore(tmpDir, logger)
	require.NoError(t, err)

	vaultID := "corrupt-test"

	// Save valid state
	err = store.Save(vaultID, &models.SyncState{
		VaultID: vaultID,
		Version: 10,
		Files:   map[string]string{"test.md": "hash"},
	})
	require.NoError(t, err)

	// Corrupt the file
	statePath := filepath.Join(tmpDir, vaultID+".json")
	err = os.WriteFile(statePath, []byte("invalid json"), 0600)
	require.NoError(t, err)

	// Should return corruption error
	_, err = store.Load(vaultID)
	assert.ErrorIs(t, err, state.ErrStateCorrupt)
}

func TestMigration(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	// Create source store
	jsonStore, err := state.NewJSONStore(filepath.Join(tmpDir, "json"), logger)
	require.NoError(t, err)
	defer jsonStore.Close()

	// Add test data
	vaults := []string{"vault1", "vault2", "vault3"}
	for i, vaultID := range vaults {
		err = jsonStore.Save(vaultID, &models.SyncState{
			VaultID: vaultID,
			Version: i * 10,
			Files: map[string]string{
				"file1.md": fmt.Sprintf("hash-%d-1", i),
				"file2.md": fmt.Sprintf("hash-%d-2", i),
			},
		})
		require.NoError(t, err)
	}

	// Create target store
	sqliteStore, err := state.NewSQLiteStore(filepath.Join(tmpDir, "state.db"), logger)
	require.NoError(t, err)
	defer sqliteStore.Close()

	// Migrate
	err = jsonStore.Migrate(sqliteStore)
	require.NoError(t, err)

	// Verify all data migrated
	migratedVaults, err := sqliteStore.List()
	require.NoError(t, err)
	assert.ElementsMatch(t, vaults, migratedVaults)

	for i, vaultID := range vaults {
		syncState, err := sqliteStore.Load(vaultID)
		require.NoError(t, err)
		assert.Equal(t, i*10, syncState.Version)
		assert.Len(t, syncState.Files, 2)
	}
}

func TestJSONStoreBackupRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	store, err := state.NewJSONStore(tmpDir, logger)
	require.NoError(t, err)
	defer store.Close()

	vaultID := "backup-test"

	// Save initial state (this creates a backup when updated)
	initialState := &models.SyncState{
		VaultID: vaultID,
		Version: 5,
		Files:   map[string]string{"init.md": "hash1"},
	}
	err = store.Save(vaultID, initialState)
	require.NoError(t, err)

	// Update state (this should create backup of first state)
	updatedState := &models.SyncState{
		VaultID: vaultID,
		Version: 10,
		Files:   map[string]string{"updated.md": "hash2"},
	}
	err = store.Save(vaultID, updatedState)
	require.NoError(t, err)

	// Verify we can load updated state
	loaded, err := store.Load(vaultID)
	require.NoError(t, err)
	assert.Equal(t, 10, loaded.Version)

	// Corrupt main file
	mainPath := filepath.Join(tmpDir, vaultID+".json")
	err = os.WriteFile(mainPath, []byte("corrupted"), 0600)
	require.NoError(t, err)

	// Should load from backup (which has the initial state)
	recovered, err := store.Load(vaultID)
	require.NoError(t, err)
	assert.Equal(t, 5, recovered.Version) // Should be the backed up state
	assert.Contains(t, recovered.Files, "init.md")
}

func TestLargeFileSet(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	sqliteStore, err := state.NewSQLiteStore(filepath.Join(tmpDir, "large.db"), logger)
	require.NoError(t, err)
	defer sqliteStore.Close()

	vaultID := "large-vault"

	// Create state with many files to test batching
	files := make(map[string]string)
	for i := 0; i < 500; i++ {
		files[fmt.Sprintf("file-%04d.md", i)] = fmt.Sprintf("hash-%d", i)
	}

	largeState := &models.SyncState{
		VaultID: vaultID,
		Version: 1000,
		Files:   files,
	}

	// Save large state
	err = sqliteStore.Save(vaultID, largeState)
	require.NoError(t, err)

	// Load and verify
	loaded, err := sqliteStore.Load(vaultID)
	require.NoError(t, err)

	assert.Equal(t, 1000, loaded.Version)
	assert.Len(t, loaded.Files, 500)
	assert.Equal(t, "hash-42", loaded.Files["file-0042.md"])
}

func TestStoreStateUpdates(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	jsonStore, err := state.NewJSONStore(tmpDir, logger)
	require.NoError(t, err)
	defer jsonStore.Close()

	vaultID := "update-test"

	// Test incremental updates
	states := []*models.SyncState{
		{
			VaultID: vaultID,
			Version: 1,
			Files:   map[string]string{"a.md": "hash-a1"},
		},
		{
			VaultID: vaultID,
			Version: 2,
			Files: map[string]string{
				"a.md": "hash-a2", // Updated
				"b.md": "hash-b1", // Added
			},
		},
		{
			VaultID: vaultID,
			Version: 3,
			Files: map[string]string{
				"b.md": "hash-b1", // a.md removed
				"c.md": "hash-c1", // c.md added
			},
		},
	}

	for i, s := range states {
		err := jsonStore.Save(vaultID, s)
		require.NoError(t, err, "Save state %d", i+1)

		loaded, err := jsonStore.Load(vaultID)
		require.NoError(t, err, "Load state %d", i+1)

		assert.Equal(t, s.Version, loaded.Version, "Version mismatch at step %d", i+1)
		assert.Equal(t, s.Files, loaded.Files, "Files mismatch at step %d", i+1)
	}
}