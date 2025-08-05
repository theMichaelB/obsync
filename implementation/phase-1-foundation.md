# Phase 1: Project Foundation

This phase establishes the project structure, initializes the Go module, and sets up core tooling.

## Directory Structure

```
obsync/
├── cmd/
│   └── obsync/
│       └── main.go              # CLI entry point
├── internal/                    # Private packages
│   ├── client/                  # High-level sync client
│   │   ├── client.go
│   │   └── client_test.go
│   ├── config/                  # Configuration management
│   │   ├── config.go
│   │   ├── loader.go
│   │   └── config_test.go
│   ├── crypto/                  # Encryption/decryption
│   │   ├── provider.go
│   │   ├── aes_gcm.go
│   │   ├── key_derive.go
│   │   ├── path_decrypt.go
│   │   └── crypto_test.go
│   ├── models/                  # Data structures
│   │   ├── vault.go
│   │   ├── file.go
│   │   ├── state.go
│   │   ├── event.go
│   │   └── errors.go
│   ├── services/                # Business logic
│   │   ├── auth/
│   │   │   ├── service.go
│   │   │   └── service_test.go
│   │   ├── vaults/
│   │   │   ├── service.go
│   │   │   └── service_test.go
│   │   └── sync/
│   │       ├── service.go
│   │       ├── engine.go
│   │       └── service_test.go
│   ├── state/                   # State persistence
│   │   ├── store.go
│   │   ├── json_store.go
│   │   ├── sqlite_store.go
│   │   └── store_test.go
│   ├── storage/                 # File system operations
│   │   ├── blob_store.go
│   │   ├── local_store.go
│   │   └── store_test.go
│   ├── transport/               # HTTP/WebSocket
│   │   ├── transport.go
│   │   ├── http_client.go
│   │   ├── websocket.go
│   │   └── transport_test.go
│   └── events/                  # Event system
│       ├── emitter.go
│       ├── types.go
│       └── emitter_test.go
├── test/                        # Integration tests
│   ├── integration/
│   │   ├── sync_test.go
│   │   └── fixtures/
│   │       ├── websocket_trace.json
│   │       └── encrypted_files/
│   └── testutil/               # Test helpers
│       ├── fixtures.go
│       └── mocks.go
├── scripts/                    # Build and utility scripts
│   ├── setup.sh
│   ├── build.sh
│   └── release.sh
├── docs/                       # Documentation
│   ├── API.md
│   ├── ARCHITECTURE.md
│   └── TROUBLESHOOTING.md
├── .github/                    # GitHub specific
│   └── workflows/
│       ├── ci.yml
│       └── release.yml
├── .golangci.yml              # Linter config
├── .gitignore
├── Makefile
├── go.mod
├── go.sum
├── README.md
└── LICENSE
```

## Go Module Initialization

```bash
# Initialize module
go mod init github.com/yourusername/obsync

# Set Go version
go mod edit -go=1.24.4
```

Initial `go.mod`:

```go
module github.com/yourusername/obsync

go 1.24.4

require (
    github.com/spf13/cobra v1.8.1
    github.com/stretchr/testify v1.9.0
    github.com/gorilla/websocket v1.5.3
    golang.org/x/crypto v0.31.0
)

require (
    github.com/davecgh/go-spew v1.1.1 // indirect
    github.com/inconshreveable/mousetrap v1.1.0 // indirect
    github.com/pmezard/go-difflib v1.0.0 // indirect
    github.com/spf13/pflag v1.0.5 // indirect
    gopkg.in/yaml.v3 v3.0.1 // indirect
)
```

## Main Entry Point

`cmd/obsync/main.go`:

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
    "github.com/yourusername/obsync/internal/config"
)

var (
    // Set by build flags
    version = "dev"
    commit  = "none"
    date    = "unknown"
)

var rootCmd = &cobra.Command{
    Use:   "obsync",
    Short: "Secure one-way synchronization for Obsidian vaults",
    Long: `Obsync is a deterministic, secure synchronization tool that replicates
Obsidian vaults from a remote service to the local filesystem.

It supports encrypted content, incremental sync, and resumable state.`,
    Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
}

func init() {
    // Global flags
    rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
    rootCmd.PersistentFlags().String("config", "", "Config file (default: $HOME/.obsync/config.json)")
    rootCmd.PersistentFlags().Bool("json", false, "Output logs in JSON format")
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
}
```

## Core Interface Definitions

`internal/transport/transport.go`:

```go
package transport

import (
    "context"
    "time"
)

// Transport defines the interface for network communication.
type Transport interface {
    // PostJSON sends a JSON request and returns the response.
    PostJSON(ctx context.Context, path string, payload interface{}) (map[string]interface{}, error)
    
    // StreamWS establishes a WebSocket connection and returns a message channel.
    StreamWS(ctx context.Context, initMsg InitMessage) (<-chan WSMessage, error)
    
    // Close closes all connections.
    Close() error
}

// InitMessage represents the WebSocket initialization message.
type InitMessage struct {
    Token   string `json:"token"`
    VaultID string `json:"vault_id"`
    Initial bool   `json:"initial"`
    Version int    `json:"version"`
}

// WSMessage represents a WebSocket message from the server.
type WSMessage struct {
    Type      string                 `json:"type"`
    UID       int                    `json:"uid"`
    Data      map[string]interface{} `json:"data"`
    Timestamp time.Time              `json:"timestamp"`
}
```

`internal/crypto/provider.go`:

```go
package crypto

// Provider defines cryptographic operations for vault sync.
type Provider interface {
    // DeriveKey derives a vault key from user credentials.
    DeriveKey(email, password string, info VaultKeyInfo) ([]byte, error)
    
    // DecryptData decrypts ciphertext using AES-GCM.
    DecryptData(ciphertext, key []byte) ([]byte, error)
    
    // DecryptPath decrypts an encrypted file path.
    DecryptPath(hexPath string, key []byte) (string, error)
}

// VaultKeyInfo contains key derivation parameters.
type VaultKeyInfo struct {
    Version           int    `json:"version"`
    EncryptionVersion int    `json:"encryption_version"`
    Salt              []byte `json:"salt"`
}
```

`internal/state/store.go`:

```go
package state

import (
    "time"
    
    "github.com/yourusername/obsync/internal/models"
)

// Store manages sync state persistence.
type Store interface {
    // Load retrieves the sync state for a vault.
    Load(vaultID string) (*models.SyncState, error)
    
    // Save persists the sync state for a vault.
    Save(vaultID string, state *models.SyncState) error
    
    // Reset removes all state for a vault.
    Reset(vaultID string) error
    
    // List returns all known vault IDs.
    List() ([]string, error)
}
```

`internal/storage/blob_store.go`:

```go
package storage

import "os"

// BlobStore manages local file operations.
type BlobStore interface {
    // Write saves data to a file path.
    Write(path string, data []byte, mode os.FileMode) error
    
    // Delete removes a file.
    Delete(path string) error
    
    // Exists checks if a file exists.
    Exists(path string) (bool, error)
    
    // EnsureDir creates a directory if it doesn't exist.
    EnsureDir(path string) error
}
```

## Initial Commands Setup

`cmd/obsync/login.go`:

```go
package main

import (
    "fmt"
    
    "github.com/spf13/cobra"
)

func init() {
    rootCmd.AddCommand(loginCmd)
}

var loginCmd = &cobra.Command{
    Use:   "login",
    Short: "Authenticate with the Obsync service",
    Long:  `Login stores authentication credentials for future sync operations.`,
    Example: `  obsync login --email user@example.com --password mypassword
  obsync login --email user@example.com --password mypassword --totp 123456`,
    RunE: runLogin,
}

func init() {
    loginCmd.Flags().StringP("email", "e", "", "Email address (required)")
    loginCmd.Flags().StringP("password", "p", "", "Password (required)")
    loginCmd.Flags().String("totp", "", "TOTP code for 2FA")
    
    loginCmd.MarkFlagRequired("email")
    loginCmd.MarkFlagRequired("password")
}

func runLogin(cmd *cobra.Command, args []string) error {
    email, _ := cmd.Flags().GetString("email")
    password, _ := cmd.Flags().GetString("password")
    totp, _ := cmd.Flags().GetString("totp")
    
    // TODO: Implement login logic
    fmt.Printf("Login with email: %s, TOTP: %v\n", email, totp != "")
    
    return nil
}
```

## Basic Model Definitions

`internal/models/vault.go`:

```go
package models

import "time"

// Vault represents a synchronized vault.
type Vault struct {
    ID             string    `json:"id"`
    Name           string    `json:"name"`
    EncryptionInfo KeyInfo   `json:"encryption_info"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}

// KeyInfo contains encryption parameters for a vault.
type KeyInfo struct {
    Version           int    `json:"version"`
    EncryptionVersion int    `json:"encryption_version"`
    Salt              string `json:"salt"` // Base64 encoded
}
```

`internal/models/state.go`:

```go
package models

import "time"

// SyncState tracks the synchronization progress for a vault.
type SyncState struct {
    VaultID      string            `json:"vault_id"`
    Version      int               `json:"version"`      // Last synced UID
    Files        map[string]string `json:"files"`        // Path -> Hash
    LastSyncTime time.Time         `json:"last_sync_time"`
    LastError    string            `json:"last_error,omitempty"`
}

// NewSyncState creates an empty sync state.
func NewSyncState(vaultID string) *SyncState {
    return &SyncState{
        VaultID: vaultID,
        Version: 0,
        Files:   make(map[string]string),
    }
}
```

`internal/models/errors.go`:

```go
package models

import "errors"

// Sentinel errors for common failures.
var (
    ErrNotAuthenticated = errors.New("not authenticated")
    ErrVaultNotFound    = errors.New("vault not found")
    ErrSyncInProgress   = errors.New("sync already in progress")
    ErrInvalidConfig    = errors.New("invalid configuration")
    ErrDecryptionFailed = errors.New("decryption failed")
)

// SyncError provides detailed sync failure information.
type SyncError struct {
    Phase   string
    VaultID string
    Path    string
    Err     error
}

func (e *SyncError) Error() string {
    if e.Path != "" {
        return fmt.Sprintf("sync %s: vault %s: %s: %v", e.Phase, e.VaultID, e.Path, e.Err)
    }
    return fmt.Sprintf("sync %s: vault %s: %v", e.Phase, e.VaultID, e.Err)
}

func (e *SyncError) Unwrap() error {
    return e.Err
}
```

## Test Structure Example

`internal/models/state_test.go`:

```go
package models_test

import (
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
            name:    "creates empty state",
            vaultID: "vault-123",
            want: &models.SyncState{
                VaultID: "vault-123",
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
            assert.Empty(t, got.Files)
        })
    }
}

func TestSyncError_Error(t *testing.T) {
    tests := []struct {
        name string
        err  *models.SyncError
        want string
    }{
        {
            name: "with path",
            err: &models.SyncError{
                Phase:   "decrypt",
                VaultID: "vault-123",
                Path:    "notes/test.md",
                Err:     models.ErrDecryptionFailed,
            },
            want: "sync decrypt: vault vault-123: notes/test.md: decryption failed",
        },
        {
            name: "without path",
            err: &models.SyncError{
                Phase:   "connect",
                VaultID: "vault-123",
                Err:     models.ErrNotAuthenticated,
            },
            want: "sync connect: vault vault-123: not authenticated",
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, tt.err.Error())
        })
    }
}
```

## .gitignore

```gitignore
# Binaries
obsync
*.exe
*.dll
*.so
*.dylib

# Test binary
*.test

# Output of go coverage
*.out
coverage.html

# Dependency directories
vendor/

# Go workspace
go.work

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db

# Project specific
.obsync/
*.log
*.pid

# Build artifacts
dist/
build/

# Local config
.env
.env.local
config.local.json
```

## Phase 1 Deliverables

1. ✅ Complete directory structure with domain-based organization
2. ✅ Go module with initial dependencies
3. ✅ CLI scaffold with cobra commands
4. ✅ Core interface definitions for all major components
5. ✅ Basic model structures with tests
6. ✅ Project follows naming conventions from Phase 0

## Verification Commands

```bash
# Verify structure
tree -I 'vendor|.git' -L 3

# Verify module
go mod tidy
go mod verify

# Build and test
make build
make test

# Check CLI
./obsync --help
./obsync login --help

# Verify linting passes
make lint
```

## Next Steps

With the project foundation in place, proceed to [Phase 2: Core Models](phase-2-models.md) to implement the complete data structures and WebSocket message specifications.