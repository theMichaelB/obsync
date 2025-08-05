# Phase 0: Standards & Infrastructure

This phase establishes coding standards, development tools, and CI/CD infrastructure for the Obsync project.

## Coding Standards

### File Organization

```
# GOOD: One responsibility per file
crypto/aes_gcm.go      # AES-GCM operations only
crypto/key_derive.go   # Key derivation only
crypto/path_decrypt.go # Path decryption only

# BAD: Mixed responsibilities
crypto/crypto.go       # Everything in one file
```

### Naming Conventions

```go
// File names: snake_case
vault_sync.go         // ✓ Good
VaultSync.go         // ✗ Bad
vaultsync.go         // ✗ Bad

// Package names: lowercase, no underscores
package vaultservice  // ✓ Good
package vault_service // ✗ Bad
package vaultService  // ✗ Bad

// Types: UpperCamelCase
type VaultManager struct {}     // ✓ Good
type vault_manager struct {}    // ✗ Bad

// Constants: UpperCamelCase
const MaxRetryAttempts = 3      // ✓ Good
const MAX_RETRY_ATTEMPTS = 3    // ✗ Bad

// Variables/Functions: lowerCamelCase
var syncState *SyncState        // ✓ Good
var sync_state *SyncState       // ✗ Bad

// Acronyms: Consistent casing
var apiURL string              // ✓ Good (not apiUrl)
type HTTPClient struct {}      // ✓ Good (not HttpClient)
```

### Function Structure

```go
// GOOD: Short, focused function with error handling
func (c *CryptoProvider) DecryptPath(encPath string, key []byte) (string, error) {
    if encPath == "" {
        return "", fmt.Errorf("decrypt path: empty input")
    }
    
    decoded, err := hex.DecodeString(encPath)
    if err != nil {
        return "", fmt.Errorf("decrypt path: invalid hex: %w", err)
    }
    
    plaintext, err := c.decrypt(decoded, key)
    if err != nil {
        return "", fmt.Errorf("decrypt path: %w", err)
    }
    
    return string(plaintext), nil
}

// BAD: Long function, poor error handling
func DecryptEverything(data map[string]string) map[string]string {
    result := make(map[string]string)
    for k, v := range data {
        // 100+ lines of mixed logic...
        if err != nil {
            panic(err) // Never panic in libraries
        }
    }
    return result
}
```

### Error Handling

```go
// Define sentinel errors
var (
    ErrVaultNotFound = errors.New("vault not found")
    ErrInvalidKey    = errors.New("invalid encryption key")
    ErrSyncInProgress = errors.New("sync already in progress")
)

// Custom error types
type SyncError struct {
    Phase   string
    VaultID string
    Err     error
}

func (e *SyncError) Error() string {
    return fmt.Sprintf("sync error in %s for vault %s: %v", e.Phase, e.VaultID, e.Err)
}

func (e *SyncError) Unwrap() error {
    return e.Err
}

// GOOD: Contextual error wrapping
func (s *SyncService) StartSync(vaultID string) error {
    vault, err := s.vaultRepo.GetByID(vaultID)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return ErrVaultNotFound
        }
        return fmt.Errorf("start sync: fetch vault: %w", err)
    }
    
    if err := s.transport.Connect(); err != nil {
        return &SyncError{
            Phase:   "connect",
            VaultID: vaultID,
            Err:     err,
        }
    }
    
    return nil
}
```

### Documentation

```go
// Package crypto provides AES-GCM encryption and decryption functions
// for Obsync vault synchronization.
//
// The package implements version 3 of the Obsync encryption protocol,
// using PBKDF2 for key derivation and AES-256-GCM for encryption.
package crypto

// CryptoProvider handles all cryptographic operations for vault sync.
// It is safe for concurrent use.
type CryptoProvider struct {
    // iterations controls PBKDF2 iteration count
    iterations int
}

// DecryptData decrypts the given ciphertext using AES-256-GCM.
//
// The ciphertext format is:
//   - Bytes 0-11: GCM nonce
//   - Bytes 12-27: GCM tag
//   - Bytes 28+: Encrypted data
//
// Returns an error if decryption fails or the format is invalid.
func (c *CryptoProvider) DecryptData(ciphertext, key []byte) ([]byte, error) {
    // Implementation
}
```

## golangci-lint Configuration

Create `.golangci.yml`:

```yaml
run:
  go: "1.24.4"
  timeout: 5m
  tests: true

linters:
  enable:
    - gofmt
    - goimports
    - golint
    - govet
    - errcheck
    - staticcheck
    - gosimple
    - ineffassign
    - typecheck
    - goconst
    - gocyclo
    - dupl
    - gosec
    - depguard
    - misspell
    - unparam
    - nakedret
    - prealloc
    - scopelint
    - gocritic
    - gochecknoinits
    - gochecknoglobals
    - godox
    - funlen
    - whitespace
    - wsl
    - goprintffuncname
    - gomnd
    - goerr113
    - gomodguard
    - godot
    - testpackage
    - nestif
    - exportloopref
    - exhaustive
    - sqlclosecheck
    - nolintlint
    - cyclop

linters-settings:
  gocyclo:
    min-complexity: 15
  dupl:
    threshold: 100
  goconst:
    min-len: 3
    min-occurrences: 3
  misspell:
    locale: US
  funlen:
    lines: 60
    statements: 40
  gomnd:
    settings:
      mnd:
        checks:
          - argument
          - case
          - condition
          - operation
          - return
          - assign
  govet:
    check-shadowing: true
  depguard:
    rules:
      main:
        deny:
          - pkg: "github.com/pkg/errors"
            desc: "Use fmt.Errorf with %w instead"

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - funlen
        - goconst
        - gomnd
    - path: cmd/
      linters:
        - gochecknoglobals
        - gochecknoinits
```

## CI/CD Pipeline

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main, staging]
  pull_request:
    branches: [main, staging]

env:
  GO_VERSION: "1.24.4"

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
      
      - name: Install golangci-lint
        run: |
          curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
          sh -s -- -b $(go env GOPATH)/bin v1.61.0
      
      - name: Run linters
        run: golangci-lint run --timeout=10m

  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
      
      - name: Run tests with coverage
        run: |
          go test -v -race -coverprofile=coverage.out ./...
          go tool cover -html=coverage.out -o coverage.html
      
      - name: Check coverage
        run: |
          COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
          echo "Total coverage: $COVERAGE%"
          if (( $(echo "$COVERAGE < 85" | bc -l) )); then
            echo "Coverage is below 85%"
            exit 1
          fi
      
      - name: Upload coverage
        uses: actions/upload-artifact@v4
        with:
          name: coverage-report
          path: coverage.html

  security:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
      
      - name: Run gosec
        run: |
          go install github.com/securego/gosec/v2/cmd/gosec@latest
          gosec -fmt=junit-xml -out=gosec-report.xml ./...
      
      - name: Run go mod tidy check
        run: |
          go mod tidy
          git diff --exit-code go.mod go.sum

  build:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
        arch: [amd64, arm64]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
      
      - name: Build binary
        run: |
          GOOS=${{ runner.os }} GOARCH=${{ matrix.arch }} \
          go build -ldflags="-s -w" -o obsync-${{ runner.os }}-${{ matrix.arch }} ./cmd/obsync
      
      - uses: actions/upload-artifact@v4
        with:
          name: obsync-${{ runner.os }}-${{ matrix.arch }}
          path: obsync-${{ runner.os }}-${{ matrix.arch }}
```

## Makefile

Create `Makefile`:

```makefile
.PHONY: all build test lint fmt clean install

GO := go
BINARY := obsync
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags="-X main.version=$(VERSION) -s -w"

all: lint test build

build:
	$(GO) build $(LDFLAGS) -o $(BINARY) ./cmd/obsync

test:
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

test-integration:
	$(GO) test -v -tags=integration ./test/integration/...

lint:
	golangci-lint run

fmt:
	$(GO) fmt ./...
	goimports -w .

clean:
	rm -f $(BINARY) coverage.out coverage.html

install: build
	cp $(BINARY) $(GOPATH)/bin/

# Development helpers
dev-setup:
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	$(GO) install github.com/securego/gosec/v2/cmd/gosec@latest
	$(GO) mod download

watch:
	@echo "Watching for changes..."
	@while true; do \
		inotifywait -qr -e modify -e create -e delete -e move . --exclude '.git|.idea|vendor' && \
		clear && \
		make test; \
	done

bench:
	$(GO) test -bench=. -benchmem ./...

coverage-html: test
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
```

## Git Hooks

Create `.githooks/pre-commit`:

```bash
#!/bin/bash
set -e

echo "Running pre-commit checks..."

# Format check
echo "Checking formatting..."
if ! make fmt; then
    echo "❌ Formatting issues found. Run 'make fmt' to fix."
    exit 1
fi

# Lint check
echo "Running linters..."
if ! make lint; then
    echo "❌ Linting failed."
    exit 1
fi

# Test check
echo "Running tests..."
if ! go test -short ./...; then
    echo "❌ Tests failed."
    exit 1
fi

echo "✅ All pre-commit checks passed!"
```

Create `.githooks/commit-msg`:

```bash
#!/bin/bash

# Conventional commit format check
commit_regex='^(feat|fix|docs|style|refactor|test|chore|perf|ci|build|revert)(\(.+\))?: .{1,50}'

if ! grep -qE "$commit_regex" "$1"; then
    echo "❌ Invalid commit message format!"
    echo "Expected format: <type>(<scope>): <subject>"
    echo "Example: feat(sync): add incremental sync support"
    echo ""
    echo "Types: feat, fix, docs, style, refactor, test, chore, perf, ci, build, revert"
    exit 1
fi
```

## Setup Script

Create `scripts/setup.sh`:

```bash
#!/bin/bash
set -e

echo "🔧 Setting up Obsync development environment..."

# Check Go version
required_version="1.24.4"
current_version=$(go version | awk '{print $3}' | sed 's/go//')

if [[ "$current_version" != "$required_version" ]]; then
    echo "❌ Go $required_version required, found $current_version"
    exit 1
fi

# Install tools
echo "📦 Installing development tools..."
make dev-setup

# Setup git hooks
echo "🪝 Setting up git hooks..."
git config core.hooksPath .githooks
chmod +x .githooks/*

# Initialize go module if needed
if [ ! -f "go.mod" ]; then
    echo "📝 Initializing Go module..."
    go mod init github.com/yourusername/obsync
fi

# Run initial checks
echo "✅ Running initial checks..."
make lint
make test

echo "🎉 Setup complete! Ready to develop."
```

## Phase 0 Deliverables

1. ✅ Complete `.golangci.yml` configuration
2. ✅ CI/CD pipeline with coverage enforcement
3. ✅ Makefile with common tasks
4. ✅ Git hooks for quality control
5. ✅ Concrete code examples for all standards
6. ✅ Setup script for new developers

## Next Steps

Run `./scripts/setup.sh` to initialize your development environment, then proceed to [Phase 1: Project Foundation](phase-1-foundation.md).