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