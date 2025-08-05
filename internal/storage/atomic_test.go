package storage_test

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/obsync/internal/events"
	"github.com/yourusername/obsync/internal/storage"
)

func TestAtomicWrites(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	store, err := storage.NewLocalStore(tmpDir, logger)
	require.NoError(t, err)

	t.Run("concurrent writes different files", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()

				// Create separate store for each goroutine to avoid logger race
				var buf bytes.Buffer
				logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
				concurrentStore, err := storage.NewLocalStore(tmpDir, logger)
				if err != nil {
					errors <- err
					return
				}

				path := fmt.Sprintf("concurrent-%d.txt", n)
				data := fmt.Sprintf("content-%d", n)

				if err := concurrentStore.Write(path, []byte(data), 0644); err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Write error: %v", err)
		}

		// Verify all files
		for i := 0; i < 10; i++ {
			path := fmt.Sprintf("concurrent-%d.txt", i)
			data, err := store.Read(path)
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("content-%d", i), string(data))
		}
	})

	t.Run("stream write with size limit", func(t *testing.T) {
		// Test within limit
		smallData := strings.NewReader(strings.Repeat("a", 1024))
		err := store.WriteStream("small.txt", smallData, 0644)
		assert.NoError(t, err)

		// Test exceeding limit
		store.SetMaxFileSize(1024) // 1KB limit
		largeData := strings.NewReader(strings.Repeat("b", 2048))
		err = store.WriteStream("large.txt", largeData, 0644)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "too large")

		// Large file should not exist
		exists, _ := store.Exists("large.txt")
		assert.False(t, exists)
	})

	t.Run("write failure cleanup", func(t *testing.T) {
		// Create a directory where we'll try to write a file
		err := store.EnsureDir("blocker")
		require.NoError(t, err)

		// Try to write a file with the same name (should fail)
		err = store.Write("blocker", []byte("data"), 0644)
		assert.Error(t, err)

		// Check no temp files left behind
		files, err := store.ListDir("")
		require.NoError(t, err)

		for _, file := range files {
			assert.False(t, strings.Contains(file.Path, ".tmp."),
				"Found temp file: %s", file.Path)
		}
	})
}

func TestDirectoryOperations(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	store, err := storage.NewLocalStore(tmpDir, logger)
	require.NoError(t, err)

	t.Run("create nested directories", func(t *testing.T) {
		err := store.EnsureDir("a/b/c/d/e")
		assert.NoError(t, err)

		// Verify all exist
		for _, dir := range []string{"a", "a/b", "a/b/c", "a/b/c/d", "a/b/c/d/e"} {
			info, err := store.Stat(dir)
			require.NoError(t, err)
			assert.True(t, info.IsDir)
		}
	})

	t.Run("clean empty directories", func(t *testing.T) {
		// Create file in nested directory
		err := store.Write("cleanup/sub/file.txt", []byte("data"), 0644)
		require.NoError(t, err)

		// Delete file
		err = store.Delete("cleanup/sub/file.txt")
		require.NoError(t, err)

		// Empty directories should be cleaned
		exists, _ := store.Exists("cleanup/sub")
		assert.False(t, exists)
		exists, _ = store.Exists("cleanup")
		assert.False(t, exists)
	})
}

func TestStreamOperations(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	store, err := storage.NewLocalStore(tmpDir, logger)
	require.NoError(t, err)

	t.Run("write and read stream", func(t *testing.T) {
		// Create large content
		content := strings.Repeat("Hello World! ", 1000)
		reader := strings.NewReader(content)

		// Write stream
		err := store.WriteStream("stream-test.txt", reader, 0644)
		require.NoError(t, err)

		// Read back
		data, err := store.Read("stream-test.txt")
		require.NoError(t, err)
		assert.Equal(t, content, string(data))

		// Check file info
		info, err := store.Stat("stream-test.txt")
		require.NoError(t, err)
		assert.Equal(t, int64(len(content)), info.Size)
	})

	t.Run("write stream error handling", func(t *testing.T) {
		// Create failing reader
		reader := &failingReader{failAt: 100}

		err := store.WriteStream("fail-test.txt", reader, 0644)
		assert.Error(t, err)

		// File should not exist
		exists, _ := store.Exists("fail-test.txt")
		assert.False(t, exists)
	})
}

// failingReader simulates IO errors during stream writing
type failingReader struct {
	read   int
	failAt int
}

func (r *failingReader) Read(p []byte) (n int, err error) {
	if r.read >= r.failAt {
		return 0, fmt.Errorf("simulated read error")
	}

	toRead := len(p)
	if r.read+toRead > r.failAt {
		toRead = r.failAt - r.read
	}

	for i := 0; i < toRead; i++ {
		p[i] = 'x'
	}

	r.read += toRead
	return toRead, nil
}