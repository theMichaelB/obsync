package models

import (
	"path/filepath"
	"strings"
	"time"
)

// FileItem represents a file in the vault.
type FileItem struct {
	Path         string    `json:"path"`
	Hash         string    `json:"hash"`
	Size         int64     `json:"size"`
	ModifiedTime time.Time `json:"modified_time"`
	IsDirectory  bool      `json:"is_directory"`
	IsBinary     bool      `json:"is_binary"`
}

// NormalizedPath returns the cleaned, forward-slash path.
func (f *FileItem) NormalizedPath() string {
	return strings.ReplaceAll(filepath.Clean(f.Path), "\\", "/")
}

// FileEvent represents a file system change.
type FileEvent struct {
	Type      FileEventType `json:"type"`
	UID       int           `json:"uid"`
	File      *FileItem     `json:"file,omitempty"`
	OldPath   string        `json:"old_path,omitempty"`
	Error     string        `json:"error,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// FileEventType defines types of file events.
type FileEventType string

const (
	FileEventCreate FileEventType = "create"
	FileEventUpdate FileEventType = "update"
	FileEventDelete FileEventType = "delete"
	FileEventMove   FileEventType = "move"
	FileEventError  FileEventType = "error"
)