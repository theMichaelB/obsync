package storage_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/obsync/internal/events"
	"github.com/yourusername/obsync/internal/storage"
)

func TestConflictStrategies(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	t.Run("overwrite strategy", func(t *testing.T) {
		store, err := storage.NewLocalStore(tmpDir, logger)
		require.NoError(t, err)
		store.SetConflictStrategy(storage.ConflictOverwrite)

		path := "conflict-test.txt"

		// Write original
		err = store.Write(path, []byte("original"), 0644)
		require.NoError(t, err)

		// Overwrite
		err = store.Write(path, []byte("new content"), 0644)
		require.NoError(t, err)

		// Verify overwritten
		data, err := store.Read(path)
		require.NoError(t, err)
		assert.Equal(t, "new content", string(data))
	})

	t.Run("rename strategy", func(t *testing.T) {
		store, err := storage.NewLocalStore(tmpDir, logger)
		require.NoError(t, err)
		store.SetConflictStrategy(storage.ConflictRename)

		path := "rename-test.txt"

		// Write original
		err = store.Write(path, []byte("original"), 0644)
		require.NoError(t, err)

		// Write conflict
		err = store.Write(path, []byte("conflict"), 0644)
		require.NoError(t, err)

		// Original should be unchanged
		data, err := store.Read(path)
		require.NoError(t, err)
		assert.Equal(t, "original", string(data))

		// Conflict file should exist
		files, err := store.ListDir("")
		require.NoError(t, err)

		conflictFound := false
		for _, file := range files {
			if strings.Contains(file.Path, "rename-test.conflict-") {
				conflictFound = true
				data, err := store.Read(file.Path)
				require.NoError(t, err)
				assert.Equal(t, "conflict", string(data))
				break
			}
		}
		assert.True(t, conflictFound, "Conflict file not found")
	})

	t.Run("error strategy", func(t *testing.T) {
		store, err := storage.NewLocalStore(tmpDir, logger)
		require.NoError(t, err)
		store.SetConflictStrategy(storage.ConflictError)

		path := "error-test.txt"

		// Write original
		err = store.Write(path, []byte("original"), 0644)
		require.NoError(t, err)

		// Write should fail
		err = store.Write(path, []byte("conflict"), 0644)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")

		// Original unchanged
		data, err := store.Read(path)
		require.NoError(t, err)
		assert.Equal(t, "original", string(data))
	})

	t.Run("skip strategy", func(t *testing.T) {
		store, err := storage.NewLocalStore(tmpDir, logger)
		require.NoError(t, err)
		store.SetConflictStrategy(storage.ConflictSkip)

		path := "skip-test.txt"

		// Write original
		err = store.Write(path, []byte("original"), 0644)
		require.NoError(t, err)

		// Write should succeed but not change file
		err = store.Write(path, []byte("skipped"), 0644)
		assert.NoError(t, err)

		// Original unchanged
		data, err := store.Read(path)
		require.NoError(t, err)
		assert.Equal(t, "original", string(data))
	})
}