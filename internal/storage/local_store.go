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

	"github.com/yourusername/obsync/internal/events"
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

// SetConflictStrategy sets the conflict resolution strategy.
func (s *LocalStore) SetConflictStrategy(strategy ConflictStrategy) {
	s.conflictStrategy = strategy
}

// SetMaxFileSize sets the maximum file size limit.
func (s *LocalStore) SetMaxFileSize(size int64) {
	s.maxFileSize = size
}

// SetBasePath changes the base directory for file operations.
func (s *LocalStore) SetBasePath(baseDir string) error {
	// Resolve absolute path
	absPath, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("resolve base directory: %w", err)
	}

	// Create base directory
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return fmt.Errorf("create base directory: %w", err)
	}

	s.baseDir = absPath
	return nil
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
		_ = file.Sync()
		file.Close()
	}

	// Rename atomically
	if err := os.Rename(tempPath, safePath); err != nil {
		_ = os.Remove(tempPath)
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

	// Check if it's a symlink and we don't allow symlinks
	if !s.allowSymlinks {
		stat, err := os.Lstat(safePath)
		if err == nil && stat.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlinks not allowed: %s", path)
		}
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