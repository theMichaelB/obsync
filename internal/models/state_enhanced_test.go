package models_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/obsync/internal/models"
)

func TestNewSyncState(t *testing.T) {
	tests := []struct {
		name    string
		vaultID string
		want    *models.SyncState
	}{
		{
			name:    "valid vault ID",
			vaultID: "vault-123",
			want: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files:   map[string]string{},
			},
		},
		{
			name:    "empty vault ID",
			vaultID: "",
			want: &models.SyncState{
				VaultID: "",
				Version: 0,
				Files:   map[string]string{},
			},
		},
		{
			name:    "vault ID with spaces",
			vaultID: "vault 123 test",
			want: &models.SyncState{
				VaultID: "vault 123 test",
				Version: 0,
				Files:   map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := models.NewSyncState(tt.vaultID)
			
			assert.Equal(t, tt.want.VaultID, got.VaultID)
			assert.Equal(t, tt.want.Version, got.Version)
			assert.NotNil(t, got.Files)
			assert.Equal(t, 0, len(got.Files))
			assert.True(t, got.LastSyncTime.IsZero())
			assert.Empty(t, got.LastError)
		})
	}
}

func TestSyncState_UpdateFile(t *testing.T) {
	tests := []struct {
		name         string
		initialFiles map[string]string
		path         string
		hash         string
		expectedLen  int
	}{
		{
			name:         "add new file to empty state",
			initialFiles: map[string]string{},
			path:         "notes/test.md",
			hash:         "abc123",
			expectedLen:  1,
		},
		{
			name: "add file to existing files",
			initialFiles: map[string]string{
				"existing.md": "def456",
			},
			path:        "notes/new.md",
			hash:        "ghi789",
			expectedLen: 2,
		},
		{
			name: "update existing file",
			initialFiles: map[string]string{
				"notes/test.md": "old_hash",
				"other.md":      "other_hash",
			},
			path:        "notes/test.md",
			hash:        "new_hash",
			expectedLen: 2,
		},
		{
			name:         "add file with empty path",
			initialFiles: map[string]string{},
			path:         "",
			hash:         "abc123",
			expectedLen:  1,
		},
		{
			name:         "add file with empty hash",
			initialFiles: map[string]string{},
			path:         "notes/test.md",
			hash:         "",
			expectedLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := models.NewSyncState("vault-123")
			
			// Set initial files
			for path, hash := range tt.initialFiles {
				state.UpdateFile(path, hash)
			}
			
			// Update the file
			state.UpdateFile(tt.path, tt.hash)
			
			assert.Equal(t, tt.expectedLen, len(state.Files))
			assert.Equal(t, tt.hash, state.Files[tt.path])
		})
	}
}

func TestSyncState_UpdateFile_NilFiles(t *testing.T) {
	state := &models.SyncState{
		VaultID: "vault-123",
		Files:   nil, // Nil files map
	}
	
	state.UpdateFile("test.md", "hash123")
	
	require.NotNil(t, state.Files)
	assert.Equal(t, "hash123", state.Files["test.md"])
}

func TestSyncState_RemoveFile(t *testing.T) {
	tests := []struct {
		name         string
		initialFiles map[string]string
		pathToRemove string
		expectedLen  int
		shouldExist  bool
	}{
		{
			name: "remove existing file",
			initialFiles: map[string]string{
				"notes/test.md": "hash1",
				"other.md":      "hash2",
			},
			pathToRemove: "notes/test.md",
			expectedLen:  1,
			shouldExist:  false,
		},
		{
			name: "remove non-existing file",
			initialFiles: map[string]string{
				"notes/test.md": "hash1",
			},
			pathToRemove: "non-existing.md",
			expectedLen:  1,
			shouldExist:  false,
		},
		{
			name:         "remove from empty state",
			initialFiles: map[string]string{},
			pathToRemove: "any.md",
			expectedLen:  0,
			shouldExist:  false,
		},
		{
			name: "remove last file",
			initialFiles: map[string]string{
				"only.md": "hash1",
			},
			pathToRemove: "only.md",
			expectedLen:  0,
			shouldExist:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := models.NewSyncState("vault-123")
			
			// Set initial files
			for path, hash := range tt.initialFiles {
				state.UpdateFile(path, hash)
			}
			
			// Remove the file
			state.RemoveFile(tt.pathToRemove)
			
			assert.Equal(t, tt.expectedLen, len(state.Files))
			_, exists := state.Files[tt.pathToRemove]
			assert.Equal(t, tt.shouldExist, exists)
		})
	}
}

func TestSyncState_RemoveFile_NilFiles(t *testing.T) {
	state := &models.SyncState{
		VaultID: "vault-123",
		Files:   nil,
	}
	
	// Should not panic with nil files
	state.RemoveFile("any.md")
	assert.Nil(t, state.Files)
}

func TestSyncState_HasFile(t *testing.T) {
	tests := []struct {
		name         string
		initialFiles map[string]string
		pathToCheck  string
		expected     bool
	}{
		{
			name: "file exists",
			initialFiles: map[string]string{
				"notes/test.md": "hash1",
				"other.md":      "hash2",
			},
			pathToCheck: "notes/test.md",
			expected:    true,
		},
		{
			name: "file does not exist",
			initialFiles: map[string]string{
				"notes/test.md": "hash1",
			},
			pathToCheck: "non-existing.md",
			expected:    false,
		},
		{
			name:         "empty state",
			initialFiles: map[string]string{},
			pathToCheck:  "any.md",
			expected:     false,
		},
		{
			name: "empty path",
			initialFiles: map[string]string{
				"": "hash1",
			},
			pathToCheck: "",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := models.NewSyncState("vault-123")
			
			for path, hash := range tt.initialFiles {
				state.UpdateFile(path, hash)
			}
			
			result := state.HasFile(tt.pathToCheck)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSyncState_HasFile_NilFiles(t *testing.T) {
	state := &models.SyncState{
		VaultID: "vault-123",
		Files:   nil,
	}
	
	result := state.HasFile("any.md")
	assert.False(t, result)
}

func TestSyncState_GetFileHash(t *testing.T) {
	tests := []struct {
		name         string
		initialFiles map[string]string
		pathToGet    string
		expected     string
	}{
		{
			name: "get existing hash",
			initialFiles: map[string]string{
				"notes/test.md": "hash123",
				"other.md":      "hash456",
			},
			pathToGet: "notes/test.md",
			expected:  "hash123",
		},
		{
			name: "get non-existing hash",
			initialFiles: map[string]string{
				"notes/test.md": "hash123",
			},
			pathToGet: "non-existing.md",
			expected:  "",
		},
		{
			name:         "empty state",
			initialFiles: map[string]string{},
			pathToGet:    "any.md",
			expected:     "",
		},
		{
			name: "empty hash value",
			initialFiles: map[string]string{
				"empty_hash.md": "",
			},
			pathToGet: "empty_hash.md",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := models.NewSyncState("vault-123")
			
			for path, hash := range tt.initialFiles {
				state.UpdateFile(path, hash)
			}
			
			result := state.GetFileHash(tt.pathToGet)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSyncState_GetFileHash_NilFiles(t *testing.T) {
	state := &models.SyncState{
		VaultID: "vault-123",
		Files:   nil,
	}
	
	result := state.GetFileHash("any.md")
	assert.Equal(t, "", result)
}

func TestSyncState_FileCount(t *testing.T) {
	tests := []struct {
		name         string
		initialFiles map[string]string
		expected     int
	}{
		{
			name:         "empty state",
			initialFiles: map[string]string{},
			expected:     0,
		},
		{
			name: "single file",
			initialFiles: map[string]string{
				"test.md": "hash1",
			},
			expected: 1,
		},
		{
			name: "multiple files",
			initialFiles: map[string]string{
				"notes/test.md": "hash1",
				"daily/today.md": "hash2",
				"attachments/img.png": "hash3",
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := models.NewSyncState("vault-123")
			
			for path, hash := range tt.initialFiles {
				state.UpdateFile(path, hash)
			}
			
			count := state.FileCount()
			assert.Equal(t, tt.expected, count)
		})
	}
}

func TestSyncState_FileCount_NilFiles(t *testing.T) {
	state := &models.SyncState{
		VaultID: "vault-123",
		Files:   nil,
	}
	
	count := state.FileCount()
	assert.Equal(t, 0, count)
}

func TestSyncState_UpdateVersion(t *testing.T) {
	state := models.NewSyncState("vault-123")
	
	beforeTime := time.Now()
	state.UpdateVersion(42)
	afterTime := time.Now()
	
	assert.Equal(t, 42, state.Version)
	assert.True(t, state.LastSyncTime.After(beforeTime) || state.LastSyncTime.Equal(beforeTime))
	assert.True(t, state.LastSyncTime.Before(afterTime) || state.LastSyncTime.Equal(afterTime))
}

func TestSyncState_SetError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "set error",
			err:      errors.New("test error message"),
			expected: "test error message",
		},
		{
			name:     "set nil error",
			err:      nil,
			expected: "",
		},
		{
			name:     "set wrapped error",
			err:      errors.New("wrapped: inner error"),
			expected: "wrapped: inner error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := models.NewSyncState("vault-123")
			
			state.SetError(tt.err)
			
			assert.Equal(t, tt.expected, state.LastError)
		})
	}
}

func TestSyncState_ClearError(t *testing.T) {
	state := models.NewSyncState("vault-123")
	state.LastError = "some error"
	
	state.ClearError()
	
	assert.Equal(t, "", state.LastError)
}

func TestSyncState_HasError(t *testing.T) {
	tests := []struct {
		name      string
		lastError string
		expected  bool
	}{
		{
			name:      "has error",
			lastError: "some error",
			expected:  true,
		},
		{
			name:      "empty error",
			lastError: "",
			expected:  false,
		},
		{
			name:      "whitespace error",
			lastError: "   \t  ",
			expected:  false,
		},
		{
			name:      "newline error",
			lastError: "\n",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := models.NewSyncState("vault-123")
			state.LastError = tt.lastError
			
			result := state.HasError()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSyncState_Validate(t *testing.T) {
	tests := []struct {
		name    string
		state   *models.SyncState
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid state",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: 42,
				Files: map[string]string{
					"notes/test.md": "a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3",
				},
			},
			wantErr: false,
		},
		{
			name: "empty vault ID",
			state: &models.SyncState{
				VaultID: "",
				Version: 0,
				Files:   map[string]string{},
			},
			wantErr: true,
			errMsg:  "vault ID is required",
		},
		{
			name: "whitespace vault ID",
			state: &models.SyncState{
				VaultID: "   \t  ",
				Version: 0,
				Files:   map[string]string{},
			},
			wantErr: true,
			errMsg:  "vault ID is required",
		},
		{
			name: "negative version",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: -1,
				Files:   map[string]string{},
			},
			wantErr: true,
			errMsg:  "version cannot be negative",
		},
		{
			name: "nil files map",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files:   nil,
			},
			wantErr: true,
			errMsg:  "files map cannot be nil",
		},
		{
			name: "empty file path",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files: map[string]string{
					"": "somehash",
				},
			},
			wantErr: true,
			errMsg:  "file path cannot be empty",
		},
		{
			name: "whitespace file path",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files: map[string]string{
					"   ": "somehash",
				},
			},
			wantErr: true,
			errMsg:  "file path cannot be empty",
		},
		{
			name: "empty file hash",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files: map[string]string{
					"test.md": "",
				},
			},
			wantErr: true,
			errMsg:  "file hash cannot be empty for path",
		},
		{
			name: "whitespace file hash",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files: map[string]string{
					"test.md": "   \t  ",
				},
			},
			wantErr: true,
			errMsg:  "file hash cannot be empty for path",
		},
		{
			name: "hash too short",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files: map[string]string{
					"test.md": "abc123", // Only 6 chars
				},
			},
			wantErr: true,
			errMsg:  "file hash has invalid length",
		},
		{
			name: "hash too long",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files: map[string]string{
					"test.md": strings.Repeat("a", 129), // 129 chars
				},
			},
			wantErr: true,
			errMsg:  "file hash has invalid length",
		},
		{
			name: "hash minimum length",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files: map[string]string{
					"test.md": "12345678", // Exactly 8 chars
				},
			},
			wantErr: false,
		},
		{
			name: "hash maximum length",
			state: &models.SyncState{
				VaultID: "vault-123",
				Version: 0,
				Files: map[string]string{
					"test.md": strings.Repeat("a", 128), // Exactly 128 chars
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.state.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSyncState_Clone(t *testing.T) {
	original := models.NewSyncState("vault-123")
	original.Version = 42
	original.LastSyncTime = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	original.LastError = "some error"
	original.UpdateFile("notes/test.md", "hash1")
	original.UpdateFile("daily/today.md", "hash2")

	clone := original.Clone()

	// Verify clone has same values
	assert.Equal(t, original.VaultID, clone.VaultID)
	assert.Equal(t, original.Version, clone.Version)
	assert.True(t, original.LastSyncTime.Equal(clone.LastSyncTime))
	assert.Equal(t, original.LastError, clone.LastError)
	assert.Equal(t, len(original.Files), len(clone.Files))

	// Verify files are copied
	for path, hash := range original.Files {
		assert.Equal(t, hash, clone.Files[path])
	}

	// Verify independence - modifying clone doesn't affect original
	clone.UpdateFile("new.md", "newhash")
	clone.Version = 100
	clone.LastError = "different error"

	assert.NotEqual(t, original.Version, clone.Version)
	assert.NotEqual(t, original.LastError, clone.LastError)
	assert.False(t, original.HasFile("new.md"))
	assert.True(t, clone.HasFile("new.md"))
}

func TestSyncState_JSON_Marshaling(t *testing.T) {
	baseTime := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)
	state := models.NewSyncState("vault-json-test")
	state.Version = 123
	state.LastSyncTime = baseTime
	state.LastError = "test error"
	state.UpdateFile("notes/test.md", "a1b2c3d4e5f6789012345678901234567890abcd")
	state.UpdateFile("daily/today.md", "f1e2d3c4b5a6789012345678901234567890bcda")

	// Test JSON marshaling
	data, err := json.Marshal(state)
	require.NoError(t, err)
	assert.Contains(t, string(data), "vault-json-test")
	assert.Contains(t, string(data), "test error")

	// Test JSON unmarshaling
	var unmarshaled models.SyncState
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, state.VaultID, unmarshaled.VaultID)
	assert.Equal(t, state.Version, unmarshaled.Version)
	assert.True(t, state.LastSyncTime.Equal(unmarshaled.LastSyncTime))
	assert.Equal(t, state.LastError, unmarshaled.LastError)
	assert.Equal(t, len(state.Files), len(unmarshaled.Files))

	for path, hash := range state.Files {
		assert.Equal(t, hash, unmarshaled.Files[path])
	}

	// Validate the unmarshaled state
	err = unmarshaled.Validate()
	assert.NoError(t, err)
}