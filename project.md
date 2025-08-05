Obsidian Vault Sync Client (obsync)

Purpose

A deterministic, secure, one-way synchronization tool written in Go that replicates an Obsidian vault from a remote service to the local filesystem. The tool supports full and incremental syncs using encryption, UID-based resumable state, and structured events. It is designed as a cross-platform CLI with testable interfaces and clean modular architecture.

Scope
	•	In scope:
	•	CLI tool for sync, login, and status
	•	HTTP and WebSocket transport
	•	UID-based incremental sync protocol
	•	AES-based decryption of encrypted content and paths
	•	Persistent local state and file storage
	•	Out of scope:
	•	Local-to-remote sync
	•	Real-time change detection
	•	Obsidian plugin integration

⸻

Architecture Overview

obsync/
├── cmd/             # CLI entry point (cobra or urfave)
├── client/          # High-level sync API
├── config/          # Config/env helpers
├── crypto/          # AES decryption and key derivation
├── models/          # Data structures
├── state/           # Local sync state store (JSON or SQLite)
├── storage/         # File system abstraction
├── services/        # Auth, vaults, and sync engine
├── transport/       # HTTP + WebSocket abstraction
├── events/          # Sync events, logging
└── internal/test/   # Fixtures, test helpers


⸻

Core Concepts

UID as Sync Cursor

Each remote event (file creation, update, deletion) is assigned a unique, increasing uid. The client tracks the last seen UID in SyncState and requests only changes above that.

Sync Modes
	•	Full sync: initial=true, version=0 → receives all content
	•	Incremental sync: initial=false, version=<last_uid> → receives only new changes

Encryption
	•	All content and paths are encrypted
	•	Client decrypts using AES and derived vault key
	•	Hashes are validated post-decryption

⸻

Key Structs

Vault

type Vault struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

VaultKeyInfo

type VaultKeyInfo struct {
    Version           int
    EncryptionVersion int
    Salt              []byte
}

SyncState

type SyncState struct {
    Version      int               // Highest UID synced
    Files        map[string]string // Path -> hash
    LastSyncTime time.Time
}


⸻

Interfaces

Transport

type Transport interface {
    PostJSON(path string, payload any) (map[string]any, error)
    StreamWS(initMessage InitMessage) (<-chan WSMessage, error)
}

CryptoProvider

type CryptoProvider interface {
    DeriveKey(email, password string, info VaultKeyInfo) ([]byte, error)
    DecryptData(ciphertext, key []byte) ([]byte, error)
    DecryptPath(hexPath string, key []byte) (string, error)
}

StateStore

type StateStore interface {
    Load(vaultID string) (*SyncState, error)
    Save(vaultID string, state *SyncState) error
    Reset(vaultID string) error
}

BlobStore

type BlobStore interface {
    Write(path string, data []byte, isBinary bool) error
    Delete(path string) error
    Exists(path string) bool
}


⸻

SyncService

API

type SyncService interface {
    FullSync(vaultID, dest string) error
    IncrementalSync(vaultID, dest string) error
}

Logic
	1.	Authenticate via token
	2.	Connect to WebSocket
	3.	Send init message:
	•	Full: initial=true, version=0
	•	Incremental: initial=false, version=last_uid
	4.	For each message:
	•	If deleted → remove file
	•	If folder → ensure exists
	•	If file:
	•	Download chunk
	•	Decrypt path and content
	•	Verify hash
	•	Write to disk
	5.	Update SyncState

⸻

CLI

Commands

obsync login --email ... --password ...
obsync vaults
obsync sync <vault-id> --dest /path/to/folder
obsync status
obsync reset


⸻

Testing Strategy
	•	Use Go test + testify
	•	Simulate sync streams with fixture JSON
	•	Validate path decryption, content decryption, state updates
	•	CLI integration tests

⸻

Implementation Plan

Step 1: Define Models
	•	Vault, VaultKeyInfo, SyncState, WS messages
	•	Goal: strongly typed structs with test coverage

Step 2: Implement CryptoProvider
	•	AES-GCM decrypt, PBKDF2, path decoding
	•	Tests: known vectors and edge case errors

Step 3: Implement Transport Layer
	•	HTTP with retry
	•	WebSocket frame parsing from fixture logs
	•	Tests: mock responses, frame parsing

Step 4: Create JSON StateStore
	•	Load/save SyncState per vault
	•	Handle versioning, corruption

Step 5: Build BlobStore
	•	Safe local writes, path normalization

Step 6: Write Auth and Vault Services
	•	Login and vault list

Step 7: Implement SyncService
	•	Full and incremental sync from WS stream
	•	Emit typed events

Step 8: Build CLI
	•	Use cobra for subcommands
	•	Progress via stdout or event log

Step 9: Integration Tests
	•	Use replayed WS sessions
	•	Validate full -> incremental cycle

⸻

Error Handling

type ObsyncError interface { Error() string }
type ApiError struct { Code int; Message string }
type DecryptError struct { Reason string }
type IntegrityError struct { File string }
type ConflictError struct { Path string }


⸻

Deployment Goals
	•	Cross-platform builds with go build
	•	Minimal external dependencies
	•	One binary, one config file, resumable vault sync

⸻

Final Notes
	•	uid is always the sync cursor
	•	initial only affects sync range, not encryption behavior
	•	All files are encrypted and must be validated
	•	The system must be resilient to interruptions and restarts
	•	Logs and errors must be structured for machine parsing (JSONL optional)

⸻

This specification is implementation-ready. Development should follow the outlined phases, using interfaces to isolate crypto, transport, and file system logic for testability and future extension.

