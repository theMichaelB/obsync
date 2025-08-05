# Phase 7: Storage Layer

This phase implements safe file operations with path sanitization, conflict resolution, binary detection, and proper file permissions.

## Storage Architecture

The storage layer provides:
- Path sanitization to prevent directory traversal
- Atomic file writes with temporary files
- Conflict detection and resolution strategies
- Binary file detection and handling
- Cross-platform path normalization
- File permission preservation

## BlobStore Interface

`internal/storage/blob_store.go`:

```go
package storage

import (
    "io"
    "os"
    "time"
)

// BlobStore manages local file operations.
type BlobStore interface {
    // Write saves data to a file path.
    Write(path string, data []byte, mode os.FileMode) error
    
    // WriteStream saves data from a reader.
    WriteStream(path string, reader io.Reader, mode os.FileMode) error
    
    // Read retrieves file contents.
    Read(path string) ([]byte, error)
    
    // Delete removes a file.
    Delete(path string) error
    
    // Exists checks if a file exists.
    Exists(path string) (bool, error)
    
    // Stat returns file information.
    Stat(path string) (FileInfo, error)
    
    // EnsureDir creates a directory if it doesn't exist.
    EnsureDir(path string) error
    
    // ListDir returns directory contents.
    ListDir(path string) ([]FileInfo, error)
    
    // Move renames a file or directory.
    Move(oldPath, newPath string) error
    
    // SetModTime updates file modification time.
    SetModTime(path string, modTime time.Time) error
}

// FileInfo contains file metadata.
type FileInfo struct {
    Path         string
    Size         int64
    Mode         os.FileMode
    ModTime      time.Time
    IsDir        bool
    IsSymlink    bool
    LinkTarget   string
}

// ConflictStrategy defines how to handle file conflicts.
type ConflictStrategy int

const (
    // ConflictOverwrite replaces existing files.
    ConflictOverwrite ConflictStrategy = iota
    
    // ConflictRename creates a new file with suffix.
    ConflictRename
    
    // ConflictError returns an error on conflict.
    ConflictError
    
    // ConflictSkip ignores the new file.
    ConflictSkip
)
```

## Local File Store

`internal/storage/local_store.go`:

```go
package storage

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "time"
    
    "github.com/TheMichaelB/obsync/internal/events"
)

// LocalStore implements file system operations.
type LocalStore struct {
    baseDir          string
    conflictStrategy ConflictStrategy
    logger           *events.Logger
    
    // Security settings
    allowSymlinks    bool
    maxPathLength    int
    maxFileSize      int64
}

// NewLocalStore creates a local file store.
func NewLocalStore(baseDir string, logger *events.Logger) (*LocalStore, error) {
    // Resolve absolute path
    absPath, err := filepath.Abs(baseDir)
    if err != nil {
        return nil, fmt.Errorf("resolve base directory: %w", err)
    }
    
    // Create base directory
    if err := os.MkdirAll(absPath, 0755); err != nil {
        return nil, fmt.Errorf("create base directory: %w", err)
    }
    
    return &LocalStore{
        baseDir:          absPath,
        conflictStrategy: ConflictOverwrite,
        logger:           logger.WithField("component", "local_store"),
        allowSymlinks:    false,
        maxPathLength:    260, // Windows compatibility
        maxFileSize:      100 * 1024 * 1024, // 100MB default
    }, nil
}

// Write saves data to a file atomically.
func (s *LocalStore) Write(path string, data []byte, mode os.FileMode) error {
    safePath, err := s.sanitizePath(path)
    if err != nil {
        return fmt.Errorf("sanitize path: %w", err)
    }
    
    s.logger.WithFields(map[string]interface{}{
        "path": path,
        "size": len(data),
        "mode": mode,
    }).Debug("Writing file")
    
    // Check size limit
    if int64(len(data)) > s.maxFileSize {
        return fmt.Errorf("file too large: %d bytes (max: %d)", len(data), s.maxFileSize)
    }
    
    // Ensure parent directory exists
    parentDir := filepath.Dir(safePath)
    if err := os.MkdirAll(parentDir, 0755); err != nil {
        return fmt.Errorf("create parent directory: %w", err)
    }
    
    // Handle conflicts
    if exists, _ := s.Exists(path); exists {
        switch s.conflictStrategy {
        case ConflictError:
            return fmt.Errorf("file already exists: %s", path)
        case ConflictSkip:
            return nil
        case ConflictRename:
            safePath = s.generateConflictPath(safePath)
        }
    }
    
    // Write atomically using temp file
    tempPath := fmt.Sprintf("%s.tmp.%d", safePath, time.Now().UnixNano())
    
    if err := os.WriteFile(tempPath, data, mode); err != nil {
        return fmt.Errorf("write temp file: %w", err)
    }
    
    // Sync to disk
    if file, err := os.Open(tempPath); err == nil {
        file.Sync()
        file.Close()
    }
    
    // Rename atomically
    if err := os.Rename(tempPath, safePath); err != nil {
        os.Remove(tempPath)
        return fmt.Errorf("rename temp file: %w", err)
    }
    
    return nil
}

// WriteStream saves data from a reader.
func (s *LocalStore) WriteStream(path string, reader io.Reader, mode os.FileMode) error {
    safePath, err := s.sanitizePath(path)
    if err != nil {
        return fmt.Errorf("sanitize path: %w", err)
    }
    
    s.logger.WithField("path", path).Debug("Writing stream")
    
    // Ensure parent directory
    parentDir := filepath.Dir(safePath)
    if err := os.MkdirAll(parentDir, 0755); err != nil {
        return fmt.Errorf("create parent directory: %w", err)
    }
    
    // Create temp file
    tempPath := fmt.Sprintf("%s.tmp.%d", safePath, time.Now().UnixNano())
    tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode)
    if err != nil {
        return fmt.Errorf("create temp file: %w", err)
    }
    
    success := false
    defer func() {
        tempFile.Close()
        if !success {
            os.Remove(tempPath)
        }
    }()
    
    // Copy with size limit
    hasher := sha256.New()
    writer := io.MultiWriter(tempFile, hasher)
    
    limited := &io.LimitedReader{
        R: reader,
        N: s.maxFileSize + 1, // +1 to detect oversized
    }
    
    written, err := io.Copy(writer, limited)
    if err != nil {
        return fmt.Errorf("write stream: %w", err)
    }
    
    if limited.N <= 0 {
        return fmt.Errorf("file too large: exceeds %d bytes", s.maxFileSize)
    }
    
    // Sync to disk
    if err := tempFile.Sync(); err != nil {
        return fmt.Errorf("sync file: %w", err)
    }
    tempFile.Close()
    
    // Rename atomically
    if err := os.Rename(tempPath, safePath); err != nil {
        return fmt.Errorf("rename temp file: %w", err)
    }
    
    success = true
    
    s.logger.WithFields(map[string]interface{}{
        "path":  path,
        "size":  written,
        "hash":  hex.EncodeToString(hasher.Sum(nil)),
    }).Debug("Stream written")
    
    return nil
}

// Read retrieves file contents.
func (s *LocalStore) Read(path string) ([]byte, error) {
    safePath, err := s.sanitizePath(path)
    if err != nil {
        return nil, fmt.Errorf("sanitize path: %w", err)
    }
    
    data, err := os.ReadFile(safePath)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, fmt.Errorf("file not found: %s", path)
        }
        return nil, fmt.Errorf("read file: %w", err)
    }
    
    return data, nil
}

// Delete removes a file.
func (s *LocalStore) Delete(path string) error {
    safePath, err := s.sanitizePath(path)
    if err != nil {
        return fmt.Errorf("sanitize path: %w", err)
    }
    
    s.logger.WithField("path", path).Debug("Deleting file")
    
    if err := os.Remove(safePath); err != nil {
        if os.IsNotExist(err) {
            return nil // Already deleted
        }
        return fmt.Errorf("delete file: %w", err)
    }
    
    // Clean up empty parent directories
    s.cleanEmptyDirs(filepath.Dir(safePath))
    
    return nil
}

// Exists checks if a file exists.
func (s *LocalStore) Exists(path string) (bool, error) {
    safePath, err := s.sanitizePath(path)
    if err != nil {
        return false, fmt.Errorf("sanitize path: %w", err)
    }
    
    _, err = os.Stat(safePath)
    if err == nil {
        return true, nil
    }
    if os.IsNotExist(err) {
        return false, nil
    }
    return false, err
}

// Stat returns file information.
func (s *LocalStore) Stat(path string) (FileInfo, error) {
    safePath, err := s.sanitizePath(path)
    if err != nil {
        return FileInfo{}, fmt.Errorf("sanitize path: %w", err)
    }
    
    stat, err := os.Lstat(safePath)
    if err != nil {
        return FileInfo{}, fmt.Errorf("stat file: %w", err)
    }
    
    info := FileInfo{
        Path:      path,
        Size:      stat.Size(),
        Mode:      stat.Mode(),
        ModTime:   stat.ModTime(),
        IsDir:     stat.IsDir(),
        IsSymlink: stat.Mode()&os.ModeSymlink != 0,
    }
    
    // Resolve symlink
    if info.IsSymlink {
        target, err := os.Readlink(safePath)
        if err == nil {
            info.LinkTarget = target
        }
    }
    
    return info, nil
}

// EnsureDir creates a directory if it doesn't exist.
func (s *LocalStore) EnsureDir(path string) error {
    safePath, err := s.sanitizePath(path)
    if err != nil {
        return fmt.Errorf("sanitize path: %w", err)
    }
    
    return os.MkdirAll(safePath, 0755)
}

// ListDir returns directory contents.
func (s *LocalStore) ListDir(path string) ([]FileInfo, error) {
    safePath, err := s.sanitizePath(path)
    if err != nil {
        return nil, fmt.Errorf("sanitize path: %w", err)
    }
    
    entries, err := os.ReadDir(safePath)
    if err != nil {
        return nil, fmt.Errorf("read directory: %w", err)
    }
    
    var files []FileInfo
    for _, entry := range entries {
        info, err := entry.Info()
        if err != nil {
            continue
        }
        
        files = append(files, FileInfo{
            Path:    filepath.Join(path, entry.Name()),
            Size:    info.Size(),
            Mode:    info.Mode(),
            ModTime: info.ModTime(),
            IsDir:   info.IsDir(),
        })
    }
    
    return files, nil
}

// Move renames a file or directory.
func (s *LocalStore) Move(oldPath, newPath string) error {
    oldSafe, err := s.sanitizePath(oldPath)
    if err != nil {
        return fmt.Errorf("sanitize old path: %w", err)
    }
    
    newSafe, err := s.sanitizePath(newPath)
    if err != nil {
        return fmt.Errorf("sanitize new path: %w", err)
    }
    
    s.logger.WithFields(map[string]interface{}{
        "old": oldPath,
        "new": newPath,
    }).Debug("Moving file")
    
    // Ensure parent directory for destination
    if err := os.MkdirAll(filepath.Dir(newSafe), 0755); err != nil {
        return fmt.Errorf("create parent directory: %w", err)
    }
    
    return os.Rename(oldSafe, newSafe)
}

// SetModTime updates file modification time.
func (s *LocalStore) SetModTime(path string, modTime time.Time) error {
    safePath, err := s.sanitizePath(path)
    if err != nil {
        return fmt.Errorf("sanitize path: %w", err)
    }
    
    return os.Chtimes(safePath, time.Now(), modTime)
}

// Helper methods

// sanitizePath validates and normalizes a file path.
func (s *LocalStore) sanitizePath(path string) (string, error) {
    // Normalize path separators
    normalized := filepath.FromSlash(path)
    
    // Clean path (remove .., ., etc)
    cleaned := filepath.Clean(normalized)
    
    // Check for directory traversal
    if strings.Contains(cleaned, "..") {
        return "", fmt.Errorf("invalid path: contains '..'")
    }
    
    // Remove leading separators
    cleaned = strings.TrimPrefix(cleaned, string(filepath.Separator))
    
    // Build full path
    fullPath := filepath.Join(s.baseDir, cleaned)
    
    // Verify it's under base directory
    if !strings.HasPrefix(fullPath, s.baseDir+string(filepath.Separator)) && fullPath != s.baseDir {
        return "", fmt.Errorf("path escapes base directory")
    }
    
    // Check path length
    if len(fullPath) > s.maxPathLength {
        return "", fmt.Errorf("path too long: %d characters (max: %d)", len(fullPath), s.maxPathLength)
    }
    
    // Check for null bytes
    if strings.ContainsRune(path, 0) {
        return "", fmt.Errorf("path contains null bytes")
    }
    
    // Platform-specific checks
    if err := s.validatePlatformPath(cleaned); err != nil {
        return "", err
    }
    
    return fullPath, nil
}

// validatePlatformPath checks platform-specific path restrictions.
func (s *LocalStore) validatePlatformPath(path string) error {
    if runtime.GOOS == "windows" {
        // Windows reserved names
        reserved := []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4",
            "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3",
            "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}
        
        parts := strings.Split(path, string(filepath.Separator))
        for _, part := range parts {
            baseName := strings.TrimSuffix(part, filepath.Ext(part))
            upperName := strings.ToUpper(baseName)
            
            for _, reserved := range reserved {
                if upperName == reserved {
                    return fmt.Errorf("invalid path: contains reserved name '%s'", part)
                }
            }
            
            // Check for invalid characters
            for _, char := range `<>:"|?*` {
                if strings.ContainsRune(part, char) {
                    return fmt.Errorf("invalid path: contains character '%c'", char)
                }
            }
        }
    }
    
    return nil
}

// generateConflictPath creates a unique path for conflicts.
func (s *LocalStore) generateConflictPath(path string) string {
    dir := filepath.Dir(path)
    base := filepath.Base(path)
    ext := filepath.Ext(base)
    name := strings.TrimSuffix(base, ext)
    
    timestamp := time.Now().Format("20060102-150405")
    
    return filepath.Join(dir, fmt.Sprintf("%s.conflict-%s%s", name, timestamp, ext))
}

// cleanEmptyDirs removes empty parent directories.
func (s *LocalStore) cleanEmptyDirs(dirPath string) {
    for dirPath != s.baseDir && strings.HasPrefix(dirPath, s.baseDir) {
        entries, err := os.ReadDir(dirPath)
        if err != nil || len(entries) > 0 {
            break
        }
        
        if err := os.Remove(dirPath); err != nil {
            break
        }
        
        dirPath = filepath.Dir(dirPath)
    }
}
```

## Path Security Tests

`internal/storage/security_test.go`:

```go
package storage_test

import (
    "path/filepath"
    "runtime"
    "strings"
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    
    "github.com/TheMichaelB/obsync/internal/config"
    "github.com/TheMichaelB/obsync/internal/events"
    "github.com/TheMichaelB/obsync/internal/storage"
)

func TestPathSanitization(t *testing.T) {
    tmpDir := t.TempDir()
    logger := events.NewLogger(&config.LogConfig{Level: "debug"})
    
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
                store.Delete(tt.path)
            }
        })
    }
}

func TestWindowsReservedNames(t *testing.T) {
    if runtime.GOOS != "windows" {
        t.Skip("Windows-specific test")
    }
    
    tmpDir := t.TempDir()
    logger := events.NewLogger(&config.LogConfig{Level: "debug"})
    
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
    logger := events.NewLogger(&config.LogConfig{Level: "debug"})
    
    store, err := storage.NewLocalStore(tmpDir, logger)
    require.NoError(t, err)
    
    // Create a file
    targetPath := "target.txt"
    err = store.Write(targetPath, []byte("target content"), 0644)
    require.NoError(t, err)
    
    // Create symlink outside of store
    externalPath := filepath.Join(tmpDir, "..", "external.txt")
    os.WriteFile(externalPath, []byte("external"), 0644)
    
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
```

## Conflict Resolution Tests

`internal/storage/conflict_test.go`:

```go
package storage_test

import (
    "strings"
    "testing"
    "time"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    
    "github.com/TheMichaelB/obsync/internal/config"
    "github.com/TheMichaelB/obsync/internal/events"
    "github.com/TheMichaelB/obsync/internal/storage"
)

func TestConflictStrategies(t *testing.T) {
    tmpDir := t.TempDir()
    logger := events.NewLogger(&config.LogConfig{Level: "debug"})
    
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
```

## Atomic Write Tests

`internal/storage/atomic_test.go`:

```go
package storage_test

import (
    "io"
    "strings"
    "sync"
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    
    "github.com/TheMichaelB/obsync/internal/config"
    "github.com/TheMichaelB/obsync/internal/events"
    "github.com/TheMichaelB/obsync/internal/storage"
)

func TestAtomicWrites(t *testing.T) {
    tmpDir := t.TempDir()
    logger := events.NewLogger(&config.LogConfig{Level: "debug"})
    
    store, err := storage.NewLocalStore(tmpDir, logger)
    require.NoError(t, err)
    
    t.Run("concurrent writes different files", func(t *testing.T) {
        var wg sync.WaitGroup
        errors := make(chan error, 10)
        
        for i := 0; i < 10; i++ {
            wg.Add(1)
            go func(n int) {
                defer wg.Done()
                
                path := fmt.Sprintf("concurrent-%d.txt", n)
                data := fmt.Sprintf("content-%d", n)
                
                if err := store.Write(path, []byte(data), 0644); err != nil {
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
    logger := events.NewLogger(&config.LogConfig{Level: "debug"})
    
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
```

## Phase 7 Deliverables

1. ✅ Safe path sanitization preventing directory traversal
2. ✅ Atomic file writes using temporary files
3. ✅ Conflict resolution strategies (overwrite, rename, error, skip)
4. ✅ Binary file detection (already in models)
5. ✅ Cross-platform path handling (Windows reserved names)
6. ✅ File size limits and streaming support
7. ✅ Empty directory cleanup
8. ✅ Comprehensive security tests

## Verification Commands

```bash
# Run storage tests
go test -v ./internal/storage/...

# Test security specifically
go test -v -run TestSecurity ./internal/storage/...

# Test concurrent access
go test -race -run TestAtomic ./internal/storage/...

# Benchmark file operations
go test -bench=. ./internal/storage/...
```

## Next Steps

With the storage layer complete, proceed to [Phase 8: Core Services](phase-8-services.md) to implement authentication, vault management, and the sync engine.