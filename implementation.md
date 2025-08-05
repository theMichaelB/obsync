Go Implementation Plan for obsync (v1.24.4)

This document defines a concrete, step-by-step implementation plan for building the obsync vault synchronization client in Go 1.24.4.

⸻

Phase 1: Project Setup

Goals:
	•	Initialize Go module
	•	Set up directory structure
	•	Integrate basic tooling

Tasks:
	•	go mod init github.com/yourname/obsync
	•	Install cobra for CLI scaffold
	•	Install stretchr/testify for testing
	•	Add .editorconfig, .golangci.yml, and Makefile

Deliverables:
	•	Working obsync binary with --help
	•	Code linting and formatting checks pass

⸻

Phase 2: Models and Types

Goals:
	•	Define strong typed structures for vaults, sync state, files, and events

Tasks:
	•	Create models/vault.go, models/file.go, models/state.go, models/event.go
	•	Use json tags and field validation rules
	•	Add initial unit tests

Deliverables:
	•	Struct definitions: Vault, VaultKeyInfo, FileItem, SyncState, SyncEvent
	•	100% type coverage; go vet and staticcheck clean

⸻

Phase 3: Config and Logging

Goals:
	•	Load configuration from env, CLI, and files
	•	Establish structured logging

Tasks:
	•	Create config/config.go
	•	Define Config struct and defaults
	•	Use viper or manual flag parsing
	•	Add log package with structured levels and optional JSONL output

Deliverables:
	•	Configurable CLI behavior
	•	Logging hooks for info/warn/error/debug

⸻

Phase 4: CryptoProvider (v3)

Goals:
	•	Implement vault key derivation and AES-GCM decryption
	•	Support decryption of encrypted paths

Tasks:
	•	crypto/v3.go: key derivation via PBKDF2-HMAC
	•	AES-GCM decryption of content
	•	Path decryption using nonce and structured key
	•	Write unit tests with fixture inputs

Deliverables:
	•	CryptoProvider interface implemented
	•	Passes all decrypt tests and error cases

⸻

Phase 5: Transport Layer

Goals:
	•	Handle HTTP + WebSocket communication
	•	Replay known message formats from fixture

Tasks:
	•	transport/http.go - JSON RPC with error wrapping
	•	transport/ws.go - message stream, InitMessage, UID filtering
	•	Add exponential backoff + ping/pong heartbeat

Deliverables:
	•	Mockable Transport interface
	•	Verified decoding of WS messages from real traces

⸻

Phase 6: State Store

Goals:
	•	Persist sync metadata and file hashes to disk
	•	Support file-based and SQLite options

Tasks:
	•	state/json.go - JSON file per vault with versioning
	•	state/sqlite.go - Optional backend
	•	Validate schema, implement migrations

Deliverables:
	•	Resilient state recovery and storage
	•	Load/save roundtrip test coverage

⸻

Phase 7: Blob Store

Goals:
	•	Write and delete files locally
	•	Enforce path sanitization and conflict prevention

Tasks:
	•	storage/local.go - handles all file I/O
	•	Safe directory creation
	•	Set file mtime if provided

Deliverables:
	•	Filesystem layer with mocked tests
	•	Prevents directory traversal and overwrites

⸻

Phase 8: Services (Auth, Vaults, Sync)

Goals:
	•	Authenticate via email/token
	•	Fetch vault metadata
	•	Drive full + incremental sync logic

Tasks:
	•	services/auth.go - password and TOTP-based login
	•	services/vaults.go - fetch vault list and encryption keys
	•	services/sync.go - main engine with UID tracking and event emission

Deliverables:
	•	SyncService with FullSync and IncrementalSync
	•	Emits events and errors appropriately

⸻

Phase 9: CLI Commands

Goals:
	•	Expose commands via cobra
	•	Use service interfaces directly

Tasks:
	•	cmd/root.go and subcommands:
	•	login
	•	vaults
	•	sync
	•	status
	•	reset
	•	Support --resume, --verbose, --json flags

Deliverables:
	•	CLI feature parity with Python plan
	•	Structured help output and error reporting

⸻

Phase 10: Testing and Fixtures

Goals:
	•	Full coverage of core logic
	•	Replayed sync sessions from captured WS logs

Tasks:
	•	Mock transport, blob, state layers
	•	Fixtures for encrypted paths and content
	•	Write roundtrip tests:
	•	First-time full sync
	•	Resume after interruption
	•	Incremental sync after change

Deliverables:
	•	go test ./... passes
	•	Stable test fixture suite

⸻

Final Delivery Goals
	•	Build artifact: single static Go binary
	•	Config: ~/.obsync/config.json or CLI flags
	•	Log: default human-readable, optional JSONL
	•	Platform: tested on macOS, Linux, Windows

⸻

Optional Next Steps
	•	Add SQLite backend
	•	Add background daemon for scheduled syncs
	•	Export sync status as JSON or Prometheus metrics
	•	Add remote index validation feature

⸻

This plan is designed to be modular and test-first. Interfaces will allow mocking for AI-powered development and functional tests. Let me know if you want scaffolding or codegen to begin.

