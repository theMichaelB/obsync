# CLAUDE.md - AI Development Guidelines

This document provides guidelines for AI-assisted development of the Obsync project. It helps AI assistants understand the project structure, coding standards, and common development patterns.

## Project Overview

Obsync is a secure, one-way synchronization tool for Obsidian vaults written in Go. Key characteristics:
- **Language**: Go 1.24+
- **Architecture**: Clean architecture with interface-based design
- **Security**: AES-256-GCM encryption with PBKDF2 key derivation
- **Protocol**: WebSocket-based incremental sync with UID tracking
- **Testing**: Target 85% unit coverage, race condition detection
- **Deployment**: CLI binary and AWS Lambda function support

## Quick Commands

### Development Workflow
```bash
# Initial setup
./scripts/setup.sh

# Build and test
make build          # Build CLI binary
make test           # Run all tests
make lint           # Run linting
make build-lambda   # Build Lambda package

# Run with race detection
go test -race ./...

# Generate coverage report
make coverage-html
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

#### Adding Lambda Functionality
1. Update handler in `internal/lambda/handler/`
2. Add AWS SDK dependencies as needed
3. Implement adapters for AWS services
4. Update Lambda config in `internal/config/lambda.go`
5. Test with `make test-lambda`

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

// Debug logging for development
logger.WithField("data", data).Debug("Processing")
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

### Interface Pattern
```go
// Define interfaces where they're used
type Storage interface {
    Read(path string) ([]byte, error)
    Write(path string, data []byte) error
}

// Implement with concrete types
type LocalStorage struct {
    basePath string
    logger   *events.Logger
}

// Accept interfaces, return concrete types
func NewService(storage Storage) *Service {
    return &Service{storage: storage}
}
```

## Architecture Guidelines

### Package Structure
```
cmd/
├── obsync/      # CLI commands
└── lambda/      # Lambda entry point

internal/
├── client/      # High-level client API
├── config/      # Configuration management
├── crypto/      # Encryption/decryption
├── events/      # Logging and events
├── lambda/      # Lambda-specific components
│   ├── adapters/    # AWS service adapters
│   ├── handler/     # Lambda event handler
│   ├── progress/    # Progress tracking
│   ├── recovery/    # Error recovery
│   └── sync/        # Lambda sync engine
├── models/      # Data structures
├── services/    # Business logic
│   ├── auth/        # Authentication
│   ├── sync/        # Sync orchestration
│   └── vaults/      # Vault management
├── state/       # State persistence
├── storage/     # File operations
└── transport/   # Network communication
```

### Dependency Flow
```
CLI/Lambda -> Client -> Services -> {Transport, State, Storage}
                           ↓
                        Models
                           ↓
                        Crypto
```

### Interface Design
- Define interfaces in the package that uses them, not the implementation
- Keep interfaces small and focused (1-5 methods)
- Use interface{} sparingly, prefer concrete types
- Mock interfaces for testing, not concrete types

## Common Pitfalls & Solutions

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

### 4. Resource Management
```go
// BAD: Forgetting to close resources
file, _ := os.Open(path)
// Missing file.Close()

// GOOD: Use defer immediately after opening
file, err := os.Open(path)
if err != nil {
    return err
}
defer file.Close()
```

## Security Considerations

### Never Do
- Log passwords, tokens, or encryption keys
- Store credentials in code or config files
- Use math/rand for cryptographic operations
- Ignore encryption/decryption errors
- Allow path traversal (../) in file operations
- Commit real credentials (use templates)

### Always Do
- Use crypto/rand for random data
- Validate and sanitize file paths
- Check hash/integrity after decryption
- Use constant-time comparison for secrets
- Clear sensitive data from memory when possible
- Use environment variables for credentials in production

## Testing Guidelines

### Unit Tests
- Mock external dependencies (network, filesystem)
- Test error cases thoroughly
- Use table-driven tests for multiple scenarios
- Verify logging output for critical operations
- Target 85% coverage minimum

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

## Lambda-Specific Guidelines

### Memory Management
- Monitor memory usage with MemoryManager
- Pause processing when memory > 80%
- Force GC when memory is critical
- Use batch processing for large operations

### Timeout Handling
- Reserve 30-second buffer before Lambda timeout
- Save progress before timeout
- Support resumable operations
- Use DynamoDB for progress tracking

### AWS Service Integration
- Use AWS SDK v2 for all services
- Implement exponential backoff for retries
- Handle throttling gracefully
- Use IAM roles, not credentials

## Debugging Tips

### Enable Debug Logging
```bash
obsync --log-level=debug sync vault-123 --dest ./vault
```

### Profile Performance
```go
import _ "net/http/pprof"

// In development builds
go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

### Trace WebSocket Messages
Set `dev.save_websocket_trace: true` in config to save WebSocket communications for debugging.

## Common Development Workflows

### Adding New Configuration Option
1. Add field to Config struct in `internal/config/config.go`
2. Set default in DefaultConfig()
3. Add validation in Validate()
4. Add environment variable mapping
5. Update config.template.json
6. Document in README.md

### Implementing New WebSocket Message Type
1. Add type constant in `models/websocket.go`
2. Define message structure
3. Add to ParseData method if needed
4. Implement handler in sync engine
5. Add test fixtures
6. Update integration tests

### Adding Encryption Support for New Version
1. Add version constant in `crypto/provider.go`
2. Implement version-specific logic in crypto methods
3. Add test vectors
4. Update version validation
5. Test with fixture data

## Code Review Checklist

Before submitting PR, ensure:
- [ ] All tests pass (`make test`)
- [ ] Linting passes (`make lint`)
- [ ] Coverage meets requirements (85% for business logic)
- [ ] Error messages provide context
- [ ] Logging includes relevant fields
- [ ] Documentation is updated (README, code comments)
- [ ] Security implications considered
- [ ] Performance impact assessed
- [ ] Cross-platform compatibility verified
- [ ] Lambda functionality unaffected (if applicable)

## Project-Specific Commands

### Authentication Flow
1. User provides email/password/TOTP
2. System validates with Obsidian API
3. Token stored in `token.json`
4. Token auto-refreshes on expiry

### Sync Process
1. Authenticate with service
2. Get vault metadata
3. Compare with local state
4. Download changed files via WebSocket
5. Decrypt and store locally
6. Update sync state

### State Management
- SQLite for local CLI storage
- DynamoDB for Lambda deployments
- JSON files for development/testing

## Resources

- [Project Specification](project.md) - Original requirements
- [Coding Standards](CODING_STANDARDS.md) - Style guide
- [Security Guidelines](SECURITY.md) - Security best practices
- [Lambda Documentation](internal/lambda/README.md) - Serverless implementation
- [Implementation Phases](implementation/) - Development roadmap

## Important Reminders

1. **Do what's asked, nothing more** - Avoid unnecessary features or files
2. **Prefer editing over creating** - Modify existing files when possible
3. **Never create documentation unless requested** - No unsolicited README files
4. **Keep security in mind** - Never log or commit credentials
5. **Test thoroughly** - Include unit tests for new features
6. **Follow patterns** - Match existing code style and structure
7. **Use interfaces** - Design for testability and flexibility
8. **Handle errors properly** - Wrap with context, don't ignore
9. **Log appropriately** - Structured logging with context
10. **Consider Lambda** - Ensure changes work in both CLI and Lambda modes