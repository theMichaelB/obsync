# Phase 2: Core Models

This phase defines all data structures, WebSocket message formats, error codes, and test fixtures for the Obsync project.

## WebSocket Message Specifications

### Connection Flow

```
Client                          Server
  |                               |
  |-------- WebSocket Connect --->|
  |                               |
  |<------- Connection Ack -------|
  |                               |
  |-------- Init Message -------->|
  |                               |
  |<------- Init Response --------|
  |                               |
  |<------- File Messages --------|
  |<------- File Messages --------|
  |<------- Done Message ---------|
  |                               |
```

### Message Types

`internal/models/websocket.go`:

```go
package models

import (
    "encoding/json"
    "time"
)

// WSMessageType defines WebSocket message types.
type WSMessageType string

const (
    // Client to Server
    WSTypeInit      WSMessageType = "init"
    WSTypeHeartbeat WSMessageType = "heartbeat"
    
    // Server to Client
    WSTypeInitResponse WSMessageType = "init_response"
    WSTypeFile        WSMessageType = "file"
    WSTypeFolder      WSMessageType = "folder"
    WSTypeDelete      WSMessageType = "delete"
    WSTypeDone        WSMessageType = "done"
    WSTypeError       WSMessageType = "error"
    WSTypePong        WSMessageType = "pong"
)

// WSMessage is the base WebSocket message structure.
type WSMessage struct {
    Type      WSMessageType   `json:"type"`
    UID       int             `json:"uid,omitempty"`
    Timestamp time.Time       `json:"timestamp"`
    Data      json.RawMessage `json:"data"`
}

// InitMessage sent by client to start sync.
type InitMessage struct {
    Token   string `json:"token"`
    VaultID string `json:"vault_id"`
    Initial bool   `json:"initial"`
    Version int    `json:"version"` // Last synced UID
}

// InitResponse from server after init.
type InitResponse struct {
    Success       bool   `json:"success"`
    TotalFiles    int    `json:"total_files"`
    StartVersion  int    `json:"start_version"`
    LatestVersion int    `json:"latest_version"`
    Message       string `json:"message,omitempty"`
}

// FileMessage for file sync events.
type FileMessage struct {
    UID          int       `json:"uid"`
    Path         string    `json:"path"`          // Encrypted hex path
    Hash         string    `json:"hash"`          // SHA-256 of plaintext
    Size         int64     `json:"size"`          // Encrypted size
    ModifiedTime time.Time `json:"modified_time"`
    Deleted      bool      `json:"deleted"`
    ChunkID      string    `json:"chunk_id,omitempty"` // For download
}

// FolderMessage for directory creation.
type FolderMessage struct {
    UID  int    `json:"uid"`
    Path string `json:"path"` // Encrypted hex path
}

// DeleteMessage for file/folder deletion.
type DeleteMessage struct {
    UID    int    `json:"uid"`
    Path   string `json:"path"`
    IsFile bool   `json:"is_file"`
}

// DoneMessage signals sync completion.
type DoneMessage struct {
    FinalVersion int    `json:"final_version"`
    TotalSynced  int    `json:"total_synced"`
    Duration     string `json:"duration"`
}

// ErrorMessage for sync errors.
type ErrorMessage struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Path    string `json:"path,omitempty"`
    Fatal   bool   `json:"fatal"`
}
```

### Message Parsing

`internal/models/websocket_parser.go`:

```go
package models

import (
    "encoding/json"
    "fmt"
)

// ParseWSMessage parses a raw WebSocket message.
func ParseWSMessage(data []byte) (*WSMessage, error) {
    var msg WSMessage
    if err := json.Unmarshal(data, &msg); err != nil {
        return nil, fmt.Errorf("parse ws message: %w", err)
    }
    return &msg, nil
}

// ParseMessageData parses the data field based on message type.
func ParseMessageData(msg *WSMessage) (interface{}, error) {
    switch msg.Type {
    case WSTypeInitResponse:
        var data InitResponse
        if err := json.Unmarshal(msg.Data, &data); err != nil {
            return nil, fmt.Errorf("parse init response: %w", err)
        }
        return &data, nil
        
    case WSTypeFile:
        var data FileMessage
        if err := json.Unmarshal(msg.Data, &data); err != nil {
            return nil, fmt.Errorf("parse file message: %w", err)
        }
        return &data, nil
        
    case WSTypeFolder:
        var data FolderMessage
        if err := json.Unmarshal(msg.Data, &data); err != nil {
            return nil, fmt.Errorf("parse folder message: %w", err)
        }
        return &data, nil
        
    case WSTypeDelete:
        var data DeleteMessage
        if err := json.Unmarshal(msg.Data, &data); err != nil {
            return nil, fmt.Errorf("parse delete message: %w", err)
        }
        return &data, nil
        
    case WSTypeDone:
        var data DoneMessage
        if err := json.Unmarshal(msg.Data, &data); err != nil {
            return nil, fmt.Errorf("parse done message: %w", err)
        }
        return &data, nil
        
    case WSTypeError:
        var data ErrorMessage
        if err := json.Unmarshal(msg.Data, &data); err != nil {
            return nil, fmt.Errorf("parse error message: %w", err)
        }
        return &data, nil
        
    default:
        return nil, fmt.Errorf("unknown message type: %s", msg.Type)
    }
}
```

## Error Codes and Types

`internal/models/errors.go`:

```go
package models

import (
    "errors"
    "fmt"
)

// Error codes for structured error handling.
const (
    ErrCodeAuth           = "AUTH_ERROR"
    ErrCodeVaultNotFound  = "VAULT_NOT_FOUND"
    ErrCodeDecryption     = "DECRYPTION_ERROR"
    ErrCodeIntegrity      = "INTEGRITY_ERROR"
    ErrCodeNetwork        = "NETWORK_ERROR"
    ErrCodeStorage        = "STORAGE_ERROR"
    ErrCodeState          = "STATE_ERROR"
    ErrCodeConfig         = "CONFIG_ERROR"
    ErrCodeRateLimit      = "RATE_LIMIT"
    ErrCodeServerError    = "SERVER_ERROR"
)

// Sentinel errors
var (
    ErrNotAuthenticated   = errors.New("not authenticated")
    ErrVaultNotFound      = errors.New("vault not found")
    ErrSyncInProgress     = errors.New("sync already in progress")
    ErrInvalidConfig      = errors.New("invalid configuration")
    ErrDecryptionFailed   = errors.New("decryption failed")
    ErrIntegrityCheckFail = errors.New("integrity check failed")
    ErrRateLimited        = errors.New("rate limited")
    ErrConnectionLost     = errors.New("connection lost")
)

// APIError represents an error from the API.
type APIError struct {
    Code       string `json:"code"`
    Message    string `json:"message"`
    StatusCode int    `json:"status_code"`
    RequestID  string `json:"request_id,omitempty"`
}

func (e *APIError) Error() string {
    return fmt.Sprintf("API error %d (%s): %s", e.StatusCode, e.Code, e.Message)
}

// SyncError provides detailed sync failure information.
type SyncError struct {
    Code    string
    Phase   string
    VaultID string
    Path    string
    Err     error
}

func (e *SyncError) Error() string {
    if e.Path != "" {
        return fmt.Sprintf("sync %s [%s]: vault %s: %s: %v", e.Phase, e.Code, e.VaultID, e.Path, e.Err)
    }
    return fmt.Sprintf("sync %s [%s]: vault %s: %v", e.Phase, e.Code, e.VaultID, e.Err)
}

func (e *SyncError) Unwrap() error {
    return e.Err
}

// DecryptError represents a decryption failure.
type DecryptError struct {
    Path   string
    Reason string
    Err    error
}

func (e *DecryptError) Error() string {
    if e.Path != "" {
        return fmt.Sprintf("decrypt %s: %s: %v", e.Path, e.Reason, e.Err)
    }
    return fmt.Sprintf("decrypt: %s: %v", e.Reason, e.Err)
}

func (e *DecryptError) Unwrap() error {
    return e.Err
}

// IntegrityError represents a hash mismatch.
type IntegrityError struct {
    Path     string
    Expected string
    Actual   string
}

func (e *IntegrityError) Error() string {
    return fmt.Sprintf("integrity check failed for %s: expected %s, got %s", 
        e.Path, e.Expected, e.Actual)
}
```

## Core Data Models

`internal/models/file.go`:

```go
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
```

`internal/models/auth.go`:

```go
package models

import "time"

// AuthRequest for login.
type AuthRequest struct {
    Email    string `json:"email"`
    Password string `json:"password"`
    TOTP     string `json:"totp,omitempty"`
}

// AuthResponse from login.
type AuthResponse struct {
    Token     string    `json:"token"`
    ExpiresAt time.Time `json:"expires_at"`
    UserID    string    `json:"user_id"`
}

// TokenInfo stores authentication details.
type TokenInfo struct {
    Token     string    `json:"token"`
    ExpiresAt time.Time `json:"expires_at"`
    Email     string    `json:"email"`
}

// IsExpired checks if the token has expired.
func (t *TokenInfo) IsExpired() bool {
    return time.Now().After(t.ExpiresAt)
}
```

## Test Fixtures

`test/testutil/fixtures.go`:

```go
package testutil

import (
    "encoding/json"
    "time"
    
    "github.com/TheMichaelB/obsync/internal/models"
)

// SampleMessages provides test WebSocket messages.
var SampleMessages = struct {
    InitResponse string
    FileMessage  string
    FolderMessage string
    DeleteMessage string
    DoneMessage  string
    ErrorMessage string
}{
    InitResponse: `{
        "type": "init_response",
        "timestamp": "2024-01-15T10:00:00Z",
        "data": {
            "success": true,
            "total_files": 42,
            "start_version": 0,
            "latest_version": 150
        }
    }`,
    
    FileMessage: `{
        "type": "file",
        "uid": 101,
        "timestamp": "2024-01-15T10:00:01Z",
        "data": {
            "uid": 101,
            "path": "6e6f7465732f746573742e6d64",
            "hash": "a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3",
            "size": 1234,
            "modified_time": "2024-01-15T09:30:00Z",
            "chunk_id": "chunk_abc123"
        }
    }`,
    
    FolderMessage: `{
        "type": "folder",
        "uid": 102,
        "timestamp": "2024-01-15T10:00:02Z",
        "data": {
            "uid": 102,
            "path": "6e6f7465732f666f6c646572"
        }
    }`,
    
    DeleteMessage: `{
        "type": "delete",
        "uid": 103,
        "timestamp": "2024-01-15T10:00:03Z",
        "data": {
            "uid": 103,
            "path": "6f6c642f66696c652e6d64",
            "is_file": true
        }
    }`,
    
    DoneMessage: `{
        "type": "done",
        "timestamp": "2024-01-15T10:00:10Z",
        "data": {
            "final_version": 150,
            "total_synced": 42,
            "duration": "10.5s"
        }
    }`,
    
    ErrorMessage: `{
        "type": "error",
        "timestamp": "2024-01-15T10:00:05Z",
        "data": {
            "code": "DECRYPTION_ERROR",
            "message": "Failed to decrypt file content",
            "path": "notes/secret.md",
            "fatal": false
        }
    }`,
}

// SampleVault provides a test vault.
func SampleVault() *models.Vault {
    return &models.Vault{
        ID:   "vault-123",
        Name: "Test Vault",
        EncryptionInfo: models.KeyInfo{
            Version:           1,
            EncryptionVersion: 3,
            Salt:             "dGVzdHNhbHQ=", // "testsalt" in base64
        },
        CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
        UpdatedAt: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
    }
}

// SampleSyncState provides test sync state.
func SampleSyncState() *models.SyncState {
    return &models.SyncState{
        VaultID: "vault-123",
        Version: 100,
        Files: map[string]string{
            "notes/test.md":     "a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3",
            "daily/2024-01.md":  "b444ac06613fc8d63795be9ad0beaf55011936ac076d87f5e685b2e1c6f6a7f0",
            "attachments/img.png": "109f4b3c50d7b0df729d299bc6f8e9ef9066971f544142b825b7fd96e3df53c1",
        },
        LastSyncTime: time.Date(2024, 1, 14, 12, 0, 0, 0, time.UTC),
    }
}
```

`test/testutil/websocket_trace.json`:

```json
{
  "trace_id": "test-sync-001",
  "recorded_at": "2024-01-15T10:00:00Z",
  "messages": [
    {
      "direction": "client_to_server",
      "timestamp": "2024-01-15T10:00:00.000Z",
      "data": {
        "type": "init",
        "data": {
          "token": "test-token-123",
          "vault_id": "vault-123",
          "initial": false,
          "version": 100
        }
      }
    },
    {
      "direction": "server_to_client",
      "timestamp": "2024-01-15T10:00:00.100Z",
      "data": {
        "type": "init_response",
        "timestamp": "2024-01-15T10:00:00.100Z",
        "data": {
          "success": true,
          "total_files": 3,
          "start_version": 100,
          "latest_version": 103
        }
      }
    },
    {
      "direction": "server_to_client",
      "timestamp": "2024-01-15T10:00:00.200Z",
      "data": {
        "type": "file",
        "uid": 101,
        "timestamp": "2024-01-15T10:00:00.200Z",
        "data": {
          "uid": 101,
          "path": "6e6f7465732f6e65772e6d64",
          "hash": "c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2",
          "size": 512,
          "modified_time": "2024-01-15T09:45:00Z",
          "chunk_id": "chunk_101"
        }
      }
    },
    {
      "direction": "server_to_client",
      "timestamp": "2024-01-15T10:00:00.300Z",
      "data": {
        "type": "delete",
        "uid": 102,
        "timestamp": "2024-01-15T10:00:00.300Z",
        "data": {
          "uid": 102,
          "path": "6f6c642f72656d6f7665642e6d64",
          "is_file": true
        }
      }
    },
    {
      "direction": "server_to_client",
      "timestamp": "2024-01-15T10:00:00.400Z",
      "data": {
        "type": "file",
        "uid": 103,
        "timestamp": "2024-01-15T10:00:00.400Z",
        "data": {
          "uid": 103,
          "path": "6e6f7465732f746573742e6d64",
          "hash": "27d7531a0c8d6f67e7a9f2e5c7a0b7c23456789abcdef0123456789abcdef012",
          "size": 1536,
          "modified_time": "2024-01-15T09:50:00Z",
          "chunk_id": "chunk_103"
        }
      }
    },
    {
      "direction": "server_to_client",
      "timestamp": "2024-01-15T10:00:00.500Z",
      "data": {
        "type": "done",
        "timestamp": "2024-01-15T10:00:00.500Z",
        "data": {
          "final_version": 103,
          "total_synced": 3,
          "duration": "0.5s"
        }
      }
    }
  ]
}
```

## Binary Detection

`internal/models/binary_detect.go`:

```go
package models

import (
    "bytes"
    "path/filepath"
    "strings"
)

// Common binary file extensions
var binaryExtensions = map[string]bool{
    ".png":  true, ".jpg":  true, ".jpeg": true, ".gif":  true, ".bmp":  true,
    ".ico":  true, ".tiff": true, ".webp": true, ".svg":  true,
    ".pdf":  true, ".doc":  true, ".docx": true, ".xls":  true, ".xlsx": true,
    ".ppt":  true, ".pptx": true, ".odt":  true, ".ods":  true, ".odp":  true,
    ".zip":  true, ".rar":  true, ".7z":   true, ".tar":  true, ".gz":   true,
    ".bz2":  true, ".xz":   true,
    ".exe":  true, ".dll":  true, ".so":   true, ".dylib": true,
    ".mp3":  true, ".mp4":  true, ".avi":  true, ".mkv":  true, ".mov":  true,
    ".wav":  true, ".flac": true, ".aac":  true, ".ogg":  true, ".wma":  true,
    ".ttf":  true, ".otf":  true, ".woff": true, ".woff2": true, ".eot":  true,
}

// IsBinaryFile detects if a file is binary based on extension or content.
func IsBinaryFile(path string, content []byte) bool {
    // Check extension first
    ext := strings.ToLower(filepath.Ext(path))
    if binaryExtensions[ext] {
        return true
    }
    
    // For unknown extensions, check content
    if len(content) == 0 {
        return false
    }
    
    // Check for null bytes in first 8KB
    checkLen := len(content)
    if checkLen > 8192 {
        checkLen = 8192
    }
    
    if bytes.IndexByte(content[:checkLen], 0) != -1 {
        return true
    }
    
    // Check for high proportion of non-printable characters
    nonPrintable := 0
    for i := 0; i < checkLen; i++ {
        b := content[i]
        if b < 32 && b != '\t' && b != '\n' && b != '\r' {
            nonPrintable++
        }
    }
    
    // If more than 30% non-printable, consider binary
    return float64(nonPrintable)/float64(checkLen) > 0.3
}
```

## Model Tests

`internal/models/websocket_test.go`:

```go
package models_test

import (
    "encoding/json"
    "testing"
    "time"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    
    "github.com/TheMichaelB/obsync/internal/models"
)

func TestParseWSMessage(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    *models.WSMessage
        wantErr bool
    }{
        {
            name: "valid file message",
            input: `{
                "type": "file",
                "uid": 123,
                "timestamp": "2024-01-15T10:00:00Z",
                "data": {"uid": 123, "path": "test"}
            }`,
            want: &models.WSMessage{
                Type:      models.WSTypeFile,
                UID:       123,
                Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
            },
        },
        {
            name:    "invalid JSON",
            input:   `{invalid}`,
            wantErr: true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := models.ParseWSMessage([]byte(tt.input))
            
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            
            require.NoError(t, err)
            assert.Equal(t, tt.want.Type, got.Type)
            assert.Equal(t, tt.want.UID, got.UID)
            assert.Equal(t, tt.want.Timestamp.Unix(), got.Timestamp.Unix())
        })
    }
}

func TestParseMessageData(t *testing.T) {
    tests := []struct {
        name    string
        msg     *models.WSMessage
        want    interface{}
        wantErr bool
    }{
        {
            name: "parse file message",
            msg: &models.WSMessage{
                Type: models.WSTypeFile,
                Data: json.RawMessage(`{
                    "uid": 123,
                    "path": "6e6f7465732f746573742e6d64",
                    "hash": "abc123",
                    "size": 1024,
                    "modified_time": "2024-01-15T10:00:00Z"
                }`),
            },
            want: &models.FileMessage{
                UID:          123,
                Path:         "6e6f7465732f746573742e6d64",
                Hash:         "abc123",
                Size:         1024,
                ModifiedTime: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
            },
        },
        {
            name: "parse error message",
            msg: &models.WSMessage{
                Type: models.WSTypeError,
                Data: json.RawMessage(`{
                    "code": "DECRYPTION_ERROR",
                    "message": "Failed to decrypt",
                    "fatal": true
                }`),
            },
            want: &models.ErrorMessage{
                Code:    "DECRYPTION_ERROR",
                Message: "Failed to decrypt",
                Fatal:   true,
            },
        },
        {
            name: "unknown message type",
            msg: &models.WSMessage{
                Type: "unknown",
                Data: json.RawMessage(`{}`),
            },
            wantErr: true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := models.ParseMessageData(tt.msg)
            
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            
            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

`internal/models/binary_detect_test.go`:

```go
package models_test

import (
    "testing"
    
    "github.com/stretchr/testify/assert"
    
    "github.com/TheMichaelB/obsync/internal/models"
)

func TestIsBinaryFile(t *testing.T) {
    tests := []struct {
        name    string
        path    string
        content []byte
        want    bool
    }{
        {
            name: "text file by extension",
            path: "notes/test.md",
            content: []byte("# Hello World"),
            want: false,
        },
        {
            name: "binary file by extension",
            path: "images/photo.jpg",
            content: []byte{0xFF, 0xD8, 0xFF},
            want: true,
        },
        {
            name: "text file with unknown extension",
            path: "file.xyz",
            content: []byte("This is plain text"),
            want: false,
        },
        {
            name: "binary content with null bytes",
            path: "file.xyz",
            content: []byte{0x00, 0x01, 0x02, 0x03},
            want: true,
        },
        {
            name: "binary content with high non-printable ratio",
            path: "file.xyz",
            content: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
            want: true,
        },
        {
            name: "empty file",
            path: "empty.txt",
            content: []byte{},
            want: false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := models.IsBinaryFile(tt.path, tt.content)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

## Phase 2 Deliverables

1. ✅ Complete WebSocket message specifications with JSON examples
2. ✅ Error codes and structured error types
3. ✅ Core data models with validation methods
4. ✅ Binary file detection logic
5. ✅ Test fixtures and sample data
6. ✅ Message parsing with error handling
7. ✅ 100% test coverage for models

## Verification Commands

```bash
# Run model tests
go test -v ./internal/models/...

# Check coverage
go test -coverprofile=coverage.out ./internal/models/...
go tool cover -func=coverage.out

# Verify JSON parsing
go run test/parse_fixtures.go

# Lint check
golangci-lint run ./internal/models/...
```

## Next Steps

With all models defined and tested, proceed to [Phase 3: Configuration & Logging](phase-3-config-logging.md) to implement environment handling and structured logging.