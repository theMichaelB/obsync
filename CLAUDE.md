# CLAUDE.md - AI Development Guidelines

This document provides guidelines for AI-assisted development of the Obsync project. It helps AI assistants understand the project structure, coding standards, and common development patterns.

## Project Overview

Obsync is a secure, one-way synchronization tool for Obsidian vaults written in Go. Key characteristics:
- **Language**: Go 1.24.4
- **Architecture**: Clean architecture with interface-based design
- **Security**: AES-256-GCM encryption with PBKDF2 key derivation
- **Protocol**: UID-based incremental sync over WebSocket
- **Testing**: 85% unit coverage, 80% mutation coverage for business logic

## Quick Commands

### Development Setup
```bash
# Initial setup
./scripts/setup.sh

# Run tests
make test

# Run linting
make lint

# Build binary
make build

# Run with race detection
go test -race ./...
```

### Common Tasks

#### Adding a New Service
1. Define interface in `internal/services/<name>/service.go`
2. Implement with proper error handling and logging
3. Add mock in `test/testutil/mocks.go`
4. Write tests achieving 85% coverage
5. Update integration tests if needed

#### Implementing a New Command
1. Create `cmd/obsync/<command>.go`
2. Use cobra for command structure
3. Support both text and JSON output
4. Add to root command in init()
5. Include examples in Long description

## Code Patterns

### Error Handling Pattern
```go
// Always wrap errors with context
if err := operation(); err != nil {
    return fmt.Errorf("service: operation failed: %w", err)
}

// Use sentinel errors for known conditions
if errors.Is(err, models.ErrVaultNotFound) {
    return ErrVaultNotFound
}

// Custom error types for rich information
return &SyncError{
    Phase:   "decrypt",
    VaultID: vaultID,
    Path:    path,
    Err:     err,
}
```

### Logging Pattern
```go
// Use structured logging with fields
logger.WithFields(map[string]interface{}{
    "vault_id": vaultID,
    "version":  version,
    "files":    fileCount,
}).Info("Starting sync")

// Log errors with context
logger.WithError(err).Error("Operation failed")
```

### Testing Pattern
```go
// Table-driven tests
tests := []struct {
    name    string
    input   Input
    want    Output
    wantErr bool
}{
    {
        name:  "valid input",
        input: Input{...},
        want:  Output{...},
    },
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got, err := Function(tt.input)
        if tt.wantErr {
            assert.Error(t, err)
            return
        }
        require.NoError(t, err)
        assert.Equal(t, tt.want, got)
    })
}
```

## Architecture Guidelines

### Package Structure
```
internal/
├── client/      # High-level client API
├── config/      # Configuration management
├── crypto/      # Encryption/decryption
├── models/      # Data structures
├── services/    # Business logic
├── state/       # State persistence
├── storage/     # File operations
├── transport/   # Network communication
└── events/      # Logging and events
```

### Interface Design
- Define interfaces in the package that uses them, not the implementation
- Keep interfaces small and focused (1-5 methods)
- Use interface{} sparingly, prefer concrete types
- Mock interfaces for testing, not concrete types

### Dependency Flow
```
CLI -> Client -> Services -> {Transport, State, Storage}
                    ↓
                 Models
                    ↓
                 Crypto
```

## Common Pitfalls

### 1. Path Handling
```go
// BAD: Platform-specific paths
path := "folder\\file.txt"

// GOOD: Use filepath for cross-platform
path := filepath.Join("folder", "file.txt")

// GOOD: Normalize for storage/comparison
normalized := strings.ReplaceAll(filepath.Clean(path), "\\", "/")
```

### 2. Concurrent Access
```go
// BAD: Unprotected map access
files[path] = hash

// GOOD: Use mutex for shared state
mu.Lock()
files[path] = hash
mu.Unlock()

// BETTER: Use sync.Map for concurrent access
var files sync.Map
files.Store(path, hash)
```

### 3. Context Propagation
```go
// BAD: Background context in service method
ctx := context.Background()

// GOOD: Accept context as first parameter
func (s *Service) Operation(ctx context.Context, ...) error {
    // Use ctx for cancellation and timeouts
}
```

## Security Considerations

### Never Do
- Log passwords, tokens, or encryption keys
- Store credentials in code or config files
- Use math/rand for cryptographic operations
- Ignore encryption/decryption errors
- Allow path traversal (../) in file operations

### Always Do
- Use crypto/rand for random data
- Validate and sanitize file paths
- Check hash/integrity after decryption
- Use constant-time comparison for secrets
- Clear sensitive data from memory when possible

## Testing Guidelines

### Unit Tests
- Mock external dependencies (network, filesystem)
- Test error cases thoroughly
- Use table-driven tests for multiple scenarios
- Verify logging output for critical operations

### Integration Tests
```go
// Skip in short mode
if testing.Short() {
    t.Skip("Skipping integration test")
}

// Use test server for network operations
server := httptest.NewServer(handler)
defer server.Close()

// Use temp directories for file operations
tmpDir := t.TempDir()
```

### Benchmarks
```go
// Reset timer after setup
b.ResetTimer()

// Report allocations
b.ReportAllocs()

// Set bytes for throughput
b.SetBytes(int64(dataSize))

// Report custom metrics
b.ReportMetric(filesPerSec, "files/sec")
```

## Performance Tips

### 1. Reuse Objects
```go
// Use sync.Pool for frequently allocated objects
var bufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}
```

### 2. Minimize Allocations
```go
// Pre-allocate slices with known capacity
files := make([]FileInfo, 0, expectedCount)

// Reuse buffers
buf := make([]byte, 4096)
for {
    n, err := reader.Read(buf)
    // Process buf[:n]
}
```

### 3. Concurrent Processing
```go
// Use worker pool for parallel operations
sem := make(chan struct{}, maxConcurrent)
for _, file := range files {
    sem <- struct{}{}
    go func(f File) {
        defer func() { <-sem }()
        processFile(f)
    }(file)
}
```

## Debugging Tips

### Enable Debug Logging
```bash
obsync --log-level=debug sync vault-123 --dest ./vault
```

### Trace WebSocket Messages
```go
// In development mode, save WebSocket trace
if cfg.Dev.SaveWebSocketTrace {
    trace := &WSTrace{
        Messages: messages,
        SavePath: cfg.Dev.TracePath,
    }
    trace.Save()
}
```

### Profile Performance
```go
import _ "net/http/pprof"

// In main.go for development builds
go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

## Common Development Workflows

### Adding Encryption Support for New Version
1. Add version constant in `crypto/provider.go`
2. Implement version-specific logic in crypto methods
3. Add test vectors in `crypto/testdata/vectors.go`
4. Update version validation
5. Test with fixture data

### Implementing New WebSocket Message Type
1. Add type constant in `models/websocket.go`
2. Define message structure
3. Add to ParseMessageData switch
4. Implement handler in sync engine
5. Add test fixtures
6. Update integration tests

### Adding New Configuration Option
1. Add field to Config struct
2. Set default in DefaultConfig()
3. Add validation in Validate()
4. Add environment variable mapping
5. Update config example
6. Document in README

## Code Review Checklist

Before submitting PR, ensure:
- [ ] All tests pass (`make test`)
- [ ] Linting passes (`make lint`)
- [ ] Coverage meets requirements
- [ ] Error messages provide context
- [ ] Logging includes relevant fields
- [ ] Documentation is updated
- [ ] Security implications considered
- [ ] Performance impact assessed
- [ ] Cross-platform compatibility verified

## Resources

- [Implementation Phases](implementation/README.md) - Detailed implementation guide
- [Coding Standards](CODING_STANDARDS.md) - Project coding standards
- [Project Specification](project.md) - Original project requirements
- [Troubleshooting Guide](troubleshooting.md) - Common issues and solutions