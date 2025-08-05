package storage_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/obsync/internal/events"
	"github.com/yourusername/obsync/internal/storage"
)

func TestPathSanitization(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	store, err := storage.NewLocalStore(tmpDir, logger)
	require.NoError(t, err)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "normal path",
			path:    "notes/test.md",
			wantErr: false,
		},
		{
			name:    "path with dots",
			path:    "notes/./test.md",
			wantErr: false, // Should be normalized
		},
		{
			name:    "parent directory traversal",
			path:    "../etc/passwd",
			wantErr: true,
		},
		{
			name:    "embedded parent traversal",
			path:    "notes/../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute path",
			path:    "/etc/passwd",
			wantErr: false, // Gets normalized to etc/passwd
		},
		{
			name:    "null bytes",
			path:    "test\x00.md",
			wantErr: true,
		},
		{
			name:    "very long path",
			path:    strings.Repeat("a", 300) + "/file.md",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Write(tt.path, []byte("test"), 0644)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "path")
			} else {
				assert.NoError(t, err)

				// Verify file was created in safe location
				exists, _ := store.Exists(tt.path)
				assert.True(t, exists)

				// Clean up
				_ = store.Delete(tt.path)
			}
		})
	}
}

func TestWindowsReservedNames(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	store, err := storage.NewLocalStore(tmpDir, logger)
	require.NoError(t, err)

	reserved := []string{"CON", "PRN", "AUX", "NUL", "COM1", "LPT1"}

	for _, name := range reserved {
		t.Run(name, func(t *testing.T) {
			// Test exact name
			err := store.Write(name+".txt", []byte("test"), 0644)
			assert.Error(t, err)

			// Test in subdirectory
			err = store.Write("folder/"+name+".txt", []byte("test"), 0644)
			assert.Error(t, err)
		})
	}

	// Test invalid characters
	invalidChars := `<>:"|?*`
	for _, char := range invalidChars {
		path := fmt.Sprintf("file%c.txt", char)
		err := store.Write(path, []byte("test"), 0644)
		assert.Error(t, err)
	}
}

func TestSymlinkHandling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Symlink test requires Unix-like OS")
	}

	tmpDir := t.TempDir()
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)

	store, err := storage.NewLocalStore(tmpDir, logger)
	require.NoError(t, err)

	// Create a file
	targetPath := "target.txt"
	err = store.Write(targetPath, []byte("target content"), 0644)
	require.NoError(t, err)

	// Create symlink outside of store
	externalPath := filepath.Join(tmpDir, "..", "external.txt")
	err = os.WriteFile(externalPath, []byte("external"), 0644)
	require.NoError(t, err)

	linkPath := filepath.Join(tmpDir, "link.txt")
	err = os.Symlink(externalPath, linkPath)
	require.NoError(t, err)

	// Try to read through symlink
	info, err := store.Stat("link.txt")
	assert.NoError(t, err)
	assert.True(t, info.IsSymlink)

	// Store should not follow symlinks by default
	_, err = store.Read("link.txt")
	assert.Error(t, err)
}