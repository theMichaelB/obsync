package storage

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// MockStore provides a mock implementation for testing.
type MockStore struct {
	mu    sync.RWMutex
	files map[string][]byte
	dirs  map[string]bool
}

// NewMockStore creates a mock blob store.
func NewMockStore() *MockStore {
	return &MockStore{
		files: make(map[string][]byte),
		dirs:  make(map[string]bool),
	}
}

// Write saves data to a file.
func (m *MockStore) Write(path string, data []byte, mode os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.files[path] = make([]byte, len(data))
	copy(m.files[path], data)
	return nil
}

// WriteStream saves data from a reader.
func (m *MockStore) WriteStream(path string, reader io.Reader, mode os.FileMode) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	return m.Write(path, data, mode)
}

// Read retrieves file contents.
func (m *MockStore) Read(path string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if data, ok := m.files[path]; ok {
		result := make([]byte, len(data))
		copy(result, data)
		return result, nil
	}

	return nil, fmt.Errorf("file not found: %s", path)
}

// Delete removes a file.
func (m *MockStore) Delete(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.files, path)
	return nil
}

// Exists checks if a file exists.
func (m *MockStore) Exists(path string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.files[path]
	return exists, nil
}

// Stat returns file information.
func (m *MockStore) Stat(path string) (FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if data, ok := m.files[path]; ok {
		return FileInfo{
			Path:    path,
			Size:    int64(len(data)),
			Mode:    0644,
			ModTime: time.Now(),
			IsDir:   false,
		}, nil
	}

	if _, ok := m.dirs[path]; ok {
		return FileInfo{
			Path:    path,
			Size:    0,
			Mode:    0755,
			ModTime: time.Now(),
			IsDir:   true,
		}, nil
	}

	return FileInfo{}, fmt.Errorf("file not found: %s", path)
}

// EnsureDir creates a directory.
func (m *MockStore) EnsureDir(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.dirs[path] = true
	return nil
}

// ListDir returns directory contents.
func (m *MockStore) ListDir(path string) ([]FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var files []FileInfo
	// For simplicity, return all files (in a real mock, we'd filter by path)
	for filePath, data := range m.files {
		files = append(files, FileInfo{
			Path:    filePath,
			Size:    int64(len(data)),
			Mode:    0644,
			ModTime: time.Now(),
			IsDir:   false,
		})
	}

	for dirPath := range m.dirs {
		files = append(files, FileInfo{
			Path:    dirPath,
			Size:    0,
			Mode:    0755,
			ModTime: time.Now(),
			IsDir:   true,
		})
	}

	return files, nil
}

// Move renames a file or directory.
func (m *MockStore) Move(oldPath, newPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if data, ok := m.files[oldPath]; ok {
		m.files[newPath] = data
		delete(m.files, oldPath)
		return nil
	}

	if _, ok := m.dirs[oldPath]; ok {
		m.dirs[newPath] = true
		delete(m.dirs, oldPath)
		return nil
	}

	return fmt.Errorf("file not found: %s", oldPath)
}

// SetModTime updates file modification time.
func (m *MockStore) SetModTime(path string, modTime time.Time) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// In a real implementation, we'd store modification times
	// For the mock, we just check if the file exists
	if _, ok := m.files[path]; ok {
		return nil
	}

	return fmt.Errorf("file not found: %s", path)
}

// Helper methods for testing

// FileExists checks if a file exists (helper for tests).
func (m *MockStore) FileExists(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.files[path]
	return exists
}

// Clear removes all files and directories.
func (m *MockStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.files = make(map[string][]byte)
	m.dirs = make(map[string]bool)
}