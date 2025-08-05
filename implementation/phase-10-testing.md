# Phase 10: Testing & Quality

This phase implements comprehensive testing strategies, benchmarks, and integration tests to ensure code quality and performance.

## Testing Strategy

The testing approach includes:
- Unit tests with 85% coverage requirement
- Integration tests with real and mock services
- Performance benchmarks for critical paths
- End-to-end sync scenarios
- Mutation testing for business logic
- Race condition detection

## Test Structure

### Test Organization

```
test/
├── integration/           # Integration tests
│   ├── sync_test.go      # Full sync scenarios
│   ├── auth_test.go      # Authentication flow
│   └── fixtures/         # Test data
│       ├── vaults/       # Sample vault data
│       ├── encrypted/    # Encrypted test files
│       └── traces/       # WebSocket traces
├── e2e/                  # End-to-end tests
│   ├── full_sync_test.go
│   ├── incremental_test.go
│   └── recovery_test.go
├── benchmark/            # Performance tests
│   ├── crypto_bench_test.go
│   ├── sync_bench_test.go
│   └── storage_bench_test.go
└── testutil/            # Test utilities
    ├── fixtures.go      # Test data generators
    ├── mocks.go        # Mock implementations
    └── helpers.go      # Test helpers
```

## Unit Test Examples

### Model Tests with Table-Driven Approach

`internal/models/vault_test.go`:

```go
package models_test

import (
    "testing"
    "time"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    
    "github.com/TheMichaelB/obsync/internal/models"
)

func TestVault_Validate(t *testing.T) {
    tests := []struct {
        name    string
        vault   models.Vault
        wantErr bool
        errMsg  string
    }{
        {
            name: "valid vault",
            vault: models.Vault{
                ID:   "vault-123",
                Name: "Test Vault",
                EncryptionInfo: models.KeyInfo{
                    Version:           1,
                    EncryptionVersion: 3,
                    Salt:             "dGVzdHNhbHQ=",
                },
            },
            wantErr: false,
        },
        {
            name: "missing ID",
            vault: models.Vault{
                Name: "Test Vault",
            },
            wantErr: true,
            errMsg:  "vault ID is required",
        },
        {
            name: "invalid encryption version",
            vault: models.Vault{
                ID:   "vault-123",
                Name: "Test Vault",
                EncryptionInfo: models.KeyInfo{
                    EncryptionVersion: 999,
                },
            },
            wantErr: true,
            errMsg:  "unsupported encryption version",
        },
        {
            name: "invalid salt encoding",
            vault: models.Vault{
                ID:   "vault-123",
                Name: "Test Vault",
                EncryptionInfo: models.KeyInfo{
                    Salt: "not-base64!",
                },
            },
            wantErr: true,
            errMsg:  "invalid salt encoding",
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.vault.Validate()
            
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

func TestSyncState_UpdateFile(t *testing.T) {
    state := models.NewSyncState("vault-123")
    
    // Add files
    state.UpdateFile("notes/test.md", "hash1")
    state.UpdateFile("daily/2024.md", "hash2")
    
    assert.Equal(t, 2, len(state.Files))
    assert.Equal(t, "hash1", state.Files["notes/test.md"])
    assert.Equal(t, "hash2", state.Files["daily/2024.md"])
    
    // Update existing
    state.UpdateFile("notes/test.md", "hash1-updated")
    assert.Equal(t, "hash1-updated", state.Files["notes/test.md"])
    
    // Remove file
    state.RemoveFile("daily/2024.md")
    assert.Equal(t, 1, len(state.Files))
    _, exists := state.Files["daily/2024.md"]
    assert.False(t, exists)
}
```

### Service Tests with Mocks

`internal/services/sync/engine_test.go`:

```go
package sync_test

import (
    "context"
    "errors"
    "testing"
    "time"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"
    
    "github.com/TheMichaelB/obsync/internal/models"
    "github.com/TheMichaelB/obsync/internal/services/sync"
    "github.com/TheMichaelB/obsync/test/testutil"
)

func TestEngine_Sync_Success(t *testing.T) {
    // Setup mocks
    mockTransport := testutil.NewMockTransport()
    mockCrypto := testutil.NewMockCryptoProvider()
    mockState := testutil.NewMockStateStore()
    mockStorage := testutil.NewMockBlobStore()
    
    engine := sync.NewEngine(
        mockTransport,
        mockCrypto,
        mockState,
        mockStorage,
        &sync.SyncConfig{
            MaxConcurrent: 2,
            ChunkSize:     1024,
        },
        testutil.TestLogger(),
    )
    
    ctx := context.Background()
    vaultID := "test-vault"
    vaultKey := testutil.TestVaultKey()
    
    // Setup expectations
    mockState.On("Load", vaultID).Return(nil, state.ErrStateNotFound).Once()
    
    // Mock WebSocket stream
    messages := []models.WSMessage{
        testutil.WSInitResponse(true, 3),
        testutil.WSFileMessage(1, "file1.md", "content1"),
        testutil.WSFileMessage(2, "file2.md", "content2"),
        testutil.WSDoneMessage(2, 2),
    }
    
    mockTransport.SetWSMessages(messages)
    mockTransport.On("StreamWS", ctx, mock.Anything).Return(nil)
    
    // Mock crypto operations
    mockCrypto.On("DecryptPath", mock.Anything, vaultKey).Return(
        func(hexPath string) string {
            return testutil.DecryptMockPath(hexPath)
        }, nil,
    )
    
    mockCrypto.On("DecryptData", mock.Anything, vaultKey).Return(
        func(data []byte) []byte {
            return testutil.DecryptMockData(data)
        }, nil,
    )
    
    // Mock storage operations
    mockStorage.On("Write", mock.Anything, mock.Anything, mock.Anything).Return(nil)
    
    // Mock state save
    mockState.On("Save", vaultID, mock.Anything).Return(nil)
    
    // Run sync
    eventCount := 0
    go func() {
        for event := range engine.Events() {
            eventCount++
            t.Logf("Event: %s", event.Type)
            
            // Verify event data
            switch event.Type {
            case sync.EventStarted:
                assert.NotNil(t, event.Progress)
            case sync.EventFileComplete:
                assert.NotNil(t, event.File)
                assert.Contains(t, []string{"file1.md", "file2.md"}, event.File.Path)
            case sync.EventCompleted:
                assert.NotNil(t, event.Progress)
                assert.Equal(t, 2, event.Progress.ProcessedFiles)
            }
        }
    }()
    
    err := engine.Sync(ctx, vaultID, vaultKey, true)
    require.NoError(t, err)
    
    // Wait for events to process
    time.Sleep(100 * time.Millisecond)
    
    // Verify expectations
    mockTransport.AssertExpectations(t)
    mockCrypto.AssertExpectations(t)
    mockState.AssertExpectations(t)
    mockStorage.AssertExpectations(t)
    
    assert.Greater(t, eventCount, 0)
    
    // Verify final state
    progress := engine.GetProgress()
    assert.Equal(t, "completed", progress.Phase)
    assert.Equal(t, 2, progress.ProcessedFiles)
    assert.Equal(t, 2, progress.TotalFiles)
}

func TestEngine_Sync_DecryptionError(t *testing.T) {
    mockTransport := testutil.NewMockTransport()
    mockCrypto := testutil.NewMockCryptoProvider()
    mockState := testutil.NewMockStateStore()
    mockStorage := testutil.NewMockBlobStore()
    
    engine := sync.NewEngine(
        mockTransport,
        mockCrypto,
        mockState,
        mockStorage,
        &sync.SyncConfig{},
        testutil.TestLogger(),
    )
    
    ctx := context.Background()
    vaultID := "test-vault"
    vaultKey := testutil.TestVaultKey()
    
    // Setup failing decryption
    mockState.On("Load", vaultID).Return(models.NewSyncState(vaultID), nil)
    
    messages := []models.WSMessage{
        testutil.WSInitResponse(true, 1),
        testutil.WSFileMessage(1, "file1.md", "content1"),
    }
    
    mockTransport.SetWSMessages(messages)
    mockCrypto.On("DecryptPath", mock.Anything, vaultKey).Return(
        "", errors.New("decryption failed"),
    )
    
    // Run sync - should fail
    err := engine.Sync(ctx, vaultID, vaultKey, false)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "decryption")
}

func TestEngine_Cancel(t *testing.T) {
    mockTransport := testutil.NewMockTransport()
    mockCrypto := testutil.NewMockCryptoProvider()
    mockState := testutil.NewMockStateStore()
    mockStorage := testutil.NewMockBlobStore()
    
    engine := sync.NewEngine(
        mockTransport,
        mockCrypto,
        mockState,
        mockStorage,
        &sync.SyncConfig{},
        testutil.TestLogger(),
    )
    
    ctx := context.Background()
    
    // Setup slow sync
    mockState.On("Load", mock.Anything).Return(models.NewSyncState("test"), nil)
    
    // Add many messages
    var messages []models.WSMessage
    messages = append(messages, testutil.WSInitResponse(true, 100))
    for i := 1; i <= 100; i++ {
        messages = append(messages, testutil.WSFileMessage(i, fmt.Sprintf("file%d.md", i), "content"))
    }
    
    mockTransport.SetWSMessages(messages)
    mockTransport.SetDelay(50 * time.Millisecond) // Slow down processing
    
    // Start sync
    syncDone := make(chan error)
    go func() {
        syncDone <- engine.Sync(ctx, "test", testutil.TestVaultKey(), true)
    }()
    
    // Cancel after starting
    time.Sleep(200 * time.Millisecond)
    engine.Cancel()
    
    // Should complete with cancellation
    err := <-syncDone
    assert.ErrorIs(t, err, context.Canceled)
}
```

## Integration Tests

### Full Sync Integration Test

`test/integration/sync_test.go`:

```go
package integration_test

import (
    "context"
    "path/filepath"
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    
    "github.com/TheMichaelB/obsync/internal/client"
    "github.com/TheMichaelB/obsync/internal/config"
    "github.com/TheMichaelB/obsync/test/testutil"
)

func TestFullSyncIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }
    
    // Setup test environment
    testDir := t.TempDir()
    fixture := testutil.LoadFixture("simple_vault")
    
    // Create test server
    server := testutil.NewTestServer()
    defer server.Close()
    
    // Configure server with test data
    server.AddVault(fixture.Vault)
    server.SetVaultKey(fixture.VaultKey)
    server.LoadWSTrace(fixture.WSTrace)
    
    // Create client
    cfg := &config.Config{
        API: config.APIConfig{
            BaseURL: server.URL,
        },
        Storage: config.StorageConfig{
            DataDir: filepath.Join(testDir, "data"),
        },
    }
    
    client, err := client.New(cfg, testutil.TestLogger())
    require.NoError(t, err)
    
    ctx := context.Background()
    
    // Login
    err = client.Auth.Login(ctx, fixture.Email, fixture.Password, "")
    require.NoError(t, err)
    
    // List vaults
    vaults, err := client.Vaults.ListVaults(ctx)
    require.NoError(t, err)
    assert.Len(t, vaults, 1)
    assert.Equal(t, fixture.Vault.ID, vaults[0].ID)
    
    // Setup sync destination
    syncDest := filepath.Join(testDir, "vault")
    client.SetStorageBase(syncDest)
    client.Sync.SetCredentials(fixture.Email, fixture.Password)
    
    // Run sync
    var events []sync.Event
    go func() {
        for event := range client.Sync.Events() {
            events = append(events, event)
        }
    }()
    
    err = client.Sync.SyncVault(ctx, fixture.Vault.ID, sync.SyncOptions{
        Initial: true,
    })
    require.NoError(t, err)
    
    // Verify files
    for _, expectedFile := range fixture.ExpectedFiles {
        path := filepath.Join(syncDest, expectedFile.Path)
        
        // Check file exists
        assert.FileExists(t, path)
        
        // Verify content
        content, err := os.ReadFile(path)
        require.NoError(t, err)
        assert.Equal(t, expectedFile.Content, string(content))
    }
    
    // Verify events
    assert.NotEmpty(t, events)
    
    var startEvent, completeEvent *sync.Event
    fileEvents := make(map[string]*sync.Event)
    
    for _, event := range events {
        switch event.Type {
        case sync.EventStarted:
            startEvent = &event
        case sync.EventCompleted:
            completeEvent = &event
        case sync.EventFileComplete:
            if event.File != nil {
                fileEvents[event.File.Path] = &event
            }
        }
    }
    
    assert.NotNil(t, startEvent)
    assert.NotNil(t, completeEvent)
    assert.Equal(t, len(fixture.ExpectedFiles), len(fileEvents))
}

func TestIncrementalSyncIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }
    
    testDir := t.TempDir()
    server := testutil.NewTestServer()
    defer server.Close()
    
    // Setup initial state
    fixture := testutil.LoadFixture("incremental_vault")
    server.AddVault(fixture.Vault)
    server.SetVaultKey(fixture.VaultKey)
    
    cfg := &config.Config{
        API: config.APIConfig{
            BaseURL: server.URL,
        },
        Storage: config.StorageConfig{
            DataDir: filepath.Join(testDir, "data"),
        },
    }
    
    client, err := client.New(cfg, testutil.TestLogger())
    require.NoError(t, err)
    
    ctx := context.Background()
    syncDest := filepath.Join(testDir, "vault")
    
    // Login and setup
    err = client.Auth.Login(ctx, fixture.Email, fixture.Password, "")
    require.NoError(t, err)
    
    client.SetStorageBase(syncDest)
    client.Sync.SetCredentials(fixture.Email, fixture.Password)
    
    // First sync - initial files
    server.LoadWSTrace(fixture.InitialTrace)
    
    err = client.Sync.SyncVault(ctx, fixture.Vault.ID, sync.SyncOptions{
        Initial: true,
    })
    require.NoError(t, err)
    
    // Verify initial files
    initialFiles := []string{"notes/first.md", "daily/2024-01-01.md"}
    for _, file := range initialFiles {
        assert.FileExists(t, filepath.Join(syncDest, file))
    }
    
    // Second sync - incremental changes
    server.LoadWSTrace(fixture.IncrementalTrace)
    
    err = client.Sync.SyncVault(ctx, fixture.Vault.ID, sync.SyncOptions{
        Initial: false,
    })
    require.NoError(t, err)
    
    // Verify changes
    assert.FileExists(t, filepath.Join(syncDest, "notes/new.md"))
    assert.NoFileExists(t, filepath.Join(syncDest, "daily/2024-01-01.md")) // Deleted
    
    // Verify modified file
    content, err := os.ReadFile(filepath.Join(syncDest, "notes/first.md"))
    require.NoError(t, err)
    assert.Contains(t, string(content), "updated content")
}
```

## Performance Benchmarks

### Crypto Performance

`test/benchmark/crypto_bench_test.go`:

```go
package benchmark

import (
    "crypto/rand"
    "testing"
    
    "github.com/TheMichaelB/obsync/internal/crypto"
)

func BenchmarkKeyDerivation(b *testing.B) {
    provider := crypto.NewProvider()
    
    info := crypto.VaultKeyInfo{
        Version:           1,
        EncryptionVersion: 3,
        Salt:             testutil.RandomSalt(),
    }
    
    b.ResetTimer()
    b.ReportAllocs()
    
    for i := 0; i < b.N; i++ {
        _, err := provider.DeriveKey("test@example.com", "password123", info)
        if err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkDecryptData(b *testing.B) {
    provider := crypto.NewProvider()
    key := make([]byte, 32)
    rand.Read(key)
    
    sizes := []int{
        1024,       // 1KB
        10240,      // 10KB
        102400,     // 100KB
        1048576,    // 1MB
    }
    
    for _, size := range sizes {
        b.Run(fmt.Sprintf("%dKB", size/1024), func(b *testing.B) {
            plaintext := make([]byte, size)
            rand.Read(plaintext)
            
            ciphertext, err := crypto.EncryptData(plaintext, key)
            if err != nil {
                b.Fatal(err)
            }
            
            b.ResetTimer()
            b.ReportAllocs()
            b.SetBytes(int64(size))
            
            for i := 0; i < b.N; i++ {
                _, err := provider.DecryptData(ciphertext, key)
                if err != nil {
                    b.Fatal(err)
                }
            }
        })
    }
}

func BenchmarkPathDecryption(b *testing.B) {
    provider := crypto.NewProvider()
    pathDecryptor := crypto.NewPathDecryptor(provider)
    key := make([]byte, 32)
    rand.Read(key)
    
    paths := []string{
        "notes/test.md",
        "daily/2024/01/15.md",
        "attachments/images/screenshot.png",
        "very/deep/nested/folder/structure/with/many/levels/file.txt",
    }
    
    // Encrypt paths
    var encPaths []string
    for _, path := range paths {
        enc, _ := testutil.EncryptPath(path, key)
        encPaths = append(encPaths, enc)
    }
    
    b.Run("NoCache", func(b *testing.B) {
        b.ReportAllocs()
        
        for i := 0; i < b.N; i++ {
            for _, encPath := range encPaths {
                _, err := provider.DecryptPath(encPath, key)
                if err != nil {
                    b.Fatal(err)
                }
            }
        }
    })
    
    b.Run("WithCache", func(b *testing.B) {
        b.ReportAllocs()
        
        for i := 0; i < b.N; i++ {
            for _, encPath := range encPaths {
                _, err := pathDecryptor.DecryptPath(encPath, key)
                if err != nil {
                    b.Fatal(err)
                }
            }
        }
    })
}
```

### Sync Performance

`test/benchmark/sync_bench_test.go`:

```go
package benchmark

import (
    "context"
    "fmt"
    "testing"
    
    "github.com/TheMichaelB/obsync/internal/services/sync"
    "github.com/TheMichaelB/obsync/test/testutil"
)

func BenchmarkSyncEngine(b *testing.B) {
    fileCounts := []int{10, 100, 1000}
    
    for _, count := range fileCounts {
        b.Run(fmt.Sprintf("%dFiles", count), func(b *testing.B) {
            // Setup
            mockTransport := testutil.NewMockTransport()
            mockCrypto := testutil.NewMockCryptoProvider()
            mockState := testutil.NewMockStateStore()
            mockStorage := testutil.NewMockBlobStore()
            
            engine := sync.NewEngine(
                mockTransport,
                mockCrypto,
                mockState,
                mockStorage,
                &sync.SyncConfig{
                    MaxConcurrent: 10,
                    ChunkSize:     1024 * 1024,
                },
                testutil.TestLogger(),
            )
            
            // Generate test messages
            messages := testutil.GenerateSyncMessages(count, 1024) // 1KB files
            mockTransport.SetWSMessages(messages)
            
            ctx := context.Background()
            vaultKey := make([]byte, 32)
            
            b.ResetTimer()
            b.ReportAllocs()
            
            for i := 0; i < b.N; i++ {
                mockTransport.Reset()
                mockState.Reset()
                mockStorage.Reset()
                
                err := engine.Sync(ctx, "bench-vault", vaultKey, true)
                if err != nil {
                    b.Fatal(err)
                }
            }
            
            b.ReportMetric(float64(count)/b.Elapsed().Seconds(), "files/sec")
        })
    }
}

func BenchmarkConcurrentFileProcessing(b *testing.B) {
    concurrencyLevels := []int{1, 5, 10, 20}
    
    for _, level := range concurrencyLevels {
        b.Run(fmt.Sprintf("Concurrent%d", level), func(b *testing.B) {
            mockTransport := testutil.NewMockTransport()
            mockCrypto := testutil.NewMockCryptoProvider()
            mockState := testutil.NewMockStateStore()
            mockStorage := testutil.NewMockBlobStore()
            
            engine := sync.NewEngine(
                mockTransport,
                mockCrypto,
                mockState,
                mockStorage,
                &sync.SyncConfig{
                    MaxConcurrent: level,
                    ChunkSize:     1024 * 1024,
                },
                testutil.TestLogger(),
            )
            
            // Generate 100 files
            messages := testutil.GenerateSyncMessages(100, 10240) // 10KB files
            mockTransport.SetWSMessages(messages)
            
            ctx := context.Background()
            vaultKey := make([]byte, 32)
            
            b.ResetTimer()
            
            for i := 0; i < b.N; i++ {
                mockTransport.Reset()
                
                err := engine.Sync(ctx, "bench-vault", vaultKey, true)
                if err != nil {
                    b.Fatal(err)
                }
            }
        })
    }
}
```

## Test Utilities

### Mock Implementations

`test/testutil/mocks.go`:

```go
package testutil

import (
    "sync"
    
    "github.com/stretchr/testify/mock"
    
    "github.com/TheMichaelB/obsync/internal/models"
)

// MockTransport mocks the transport interface.
type MockTransport struct {
    mock.Mock
    mu       sync.Mutex
    messages []models.WSMessage
    delay    time.Duration
    index    int
}

func NewMockTransport() *MockTransport {
    return &MockTransport{}
}

func (m *MockTransport) PostJSON(ctx context.Context, path string, payload interface{}) (map[string]interface{}, error) {
    args := m.Called(ctx, path, payload)
    
    if result := args.Get(0); result != nil {
        return result.(map[string]interface{}), args.Error(1)
    }
    return nil, args.Error(1)
}

func (m *MockTransport) StreamWS(ctx context.Context, initMsg models.InitMessage) (<-chan models.WSMessage, error) {
    args := m.Called(ctx, initMsg)
    
    if args.Error(1) != nil {
        return nil, args.Error(1)
    }
    
    // Create message channel
    msgChan := make(chan models.WSMessage, len(m.messages))
    
    go func() {
        defer close(msgChan)
        
        for _, msg := range m.messages {
            select {
            case <-ctx.Done():
                return
            case msgChan <- msg:
                if m.delay > 0 {
                    time.Sleep(m.delay)
                }
            }
        }
    }()
    
    return msgChan, nil
}

func (m *MockTransport) SetWSMessages(messages []models.WSMessage) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.messages = messages
    m.index = 0
}

func (m *MockTransport) SetDelay(delay time.Duration) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.delay = delay
}

func (m *MockTransport) Reset() {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.index = 0
}

// MockCryptoProvider mocks crypto operations.
type MockCryptoProvider struct {
    mock.Mock
}

func NewMockCryptoProvider() *MockCryptoProvider {
    return &MockCryptoProvider{}
}

func (m *MockCryptoProvider) DeriveKey(email, password string, info crypto.VaultKeyInfo) ([]byte, error) {
    args := m.Called(email, password, info)
    if key := args.Get(0); key != nil {
        return key.([]byte), args.Error(1)
    }
    return nil, args.Error(1)
}

func (m *MockCryptoProvider) DecryptData(ciphertext, key []byte) ([]byte, error) {
    args := m.Called(ciphertext, key)
    if data := args.Get(0); data != nil {
        return data.([]byte), args.Error(1)
    }
    return nil, args.Error(1)
}

func (m *MockCryptoProvider) DecryptPath(hexPath string, key []byte) (string, error) {
    args := m.Called(hexPath, key)
    return args.String(0), args.Error(1)
}
```

### Test Fixtures

`test/testutil/fixtures.go`:

```go
package testutil

import (
    "encoding/json"
    "os"
    "path/filepath"
    
    "github.com/TheMichaelB/obsync/internal/models"
)

// Fixture represents test vault data.
type Fixture struct {
    Vault         *models.Vault
    VaultKey      []byte
    Email         string
    Password      string
    WSTrace       string
    ExpectedFiles []ExpectedFile
}

// ExpectedFile describes expected sync result.
type ExpectedFile struct {
    Path    string
    Content string
    Hash    string
}

// LoadFixture loads test fixture data.
func LoadFixture(name string) *Fixture {
    path := filepath.Join("fixtures", name, "fixture.json")
    
    data, err := os.ReadFile(path)
    if err != nil {
        panic(fmt.Errorf("load fixture %s: %w", name, err))
    }
    
    var fixture Fixture
    if err := json.Unmarshal(data, &fixture); err != nil {
        panic(fmt.Errorf("parse fixture %s: %w", name, err))
    }
    
    // Load WebSocket trace
    tracePath := filepath.Join("fixtures", name, "ws_trace.json")
    traceData, err := os.ReadFile(tracePath)
    if err != nil {
        panic(fmt.Errorf("load trace %s: %w", name, err))
    }
    fixture.WSTrace = string(traceData)
    
    return &fixture
}

// GenerateSyncMessages creates test WebSocket messages.
func GenerateSyncMessages(fileCount int, fileSize int) []models.WSMessage {
    messages := []models.WSMessage{
        {
            Type: models.WSTypeInitResponse,
            Data: mustMarshal(models.InitResponse{
                Success:    true,
                TotalFiles: fileCount,
            }),
        },
    }
    
    for i := 1; i <= fileCount; i++ {
        content := make([]byte, fileSize)
        rand.Read(content)
        
        messages = append(messages, models.WSMessage{
            Type: models.WSTypeFile,
            UID:  i,
            Data: mustMarshal(models.FileMessage{
                UID:     i,
                Path:    fmt.Sprintf("file%d.dat", i),
                Hash:    fmt.Sprintf("%x", sha256.Sum256(content)),
                Size:    int64(fileSize),
                ChunkID: fmt.Sprintf("chunk-%d", i),
            }),
        })
    }
    
    messages = append(messages, models.WSMessage{
        Type: models.WSTypeDone,
        Data: mustMarshal(models.DoneMessage{
            FinalVersion: fileCount,
            TotalSynced:  fileCount,
        }),
    })
    
    return messages
}

func mustMarshal(v interface{}) json.RawMessage {
    data, err := json.Marshal(v)
    if err != nil {
        panic(err)
    }
    return json.RawMessage(data)
}
```

## Coverage and Quality Tools

### Coverage Script

`scripts/coverage.sh`:

```bash
#!/bin/bash
set -e

echo "🧪 Running tests with coverage..."

# Clean previous coverage
rm -f coverage.out coverage.html

# Run tests with coverage
go test -v -race -coverprofile=coverage.out -covermode=atomic ./...

# Generate HTML report
go tool cover -html=coverage.out -o coverage.html

# Check coverage threshold
COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
echo "📊 Total coverage: $COVERAGE%"

# Business logic coverage (services, crypto, sync)
BUSINESS_COV=$(go tool cover -func=coverage.out | grep -E "(services|crypto|sync)" | \
    awk '{sum+=$3; count++} END {print sum/count}')
echo "📊 Business logic coverage: $BUSINESS_COV%"

# Check thresholds
if (( $(echo "$COVERAGE < 85" | bc -l) )); then
    echo "❌ Coverage is below 85% threshold"
    exit 1
fi

if (( $(echo "$BUSINESS_COV < 80" | bc -l) )); then
    echo "❌ Business logic coverage is below 80% threshold"
    exit 1
fi

echo "✅ Coverage requirements met!"
```

### Mutation Testing

`scripts/mutation.sh`:

```bash
#!/bin/bash
set -e

echo "🧬 Running mutation tests..."

# Install go-mutesting if needed
if ! command -v go-mutesting &> /dev/null; then
    go install github.com/zimmski/go-mutesting/cmd/go-mutesting@latest
fi

# Run mutation tests on business logic
go-mutesting ./internal/services/... ./internal/crypto/... \
    --exec "go test" \
    --blacklist ".*_test.go" \
    > mutation-report.txt

# Parse results
SCORE=$(grep "Score:" mutation-report.txt | awk '{print $2}')
echo "🧬 Mutation score: $SCORE"

# Check threshold
if (( $(echo "$SCORE < 0.8" | bc -l) )); then
    echo "❌ Mutation score is below 80% threshold"
    exit 1
fi

echo "✅ Mutation testing passed!"
```

## CI Integration

### GitHub Actions Test Workflow

`.github/workflows/test.yml`:

```yaml
name: Tests

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        go: ['1.24.4']
    
    steps:
    - uses: actions/checkout@v4
    
    - uses: actions/setup-go@v5
      with:
        go-version: ${{ matrix.go }}
    
    - name: Cache Go modules
      uses: actions/cache@v4
      with:
        path: ~/go/pkg/mod
        key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
    
    - name: Run unit tests
      run: |
        go test -v -race -coverprofile=coverage.out ./...
        go tool cover -func=coverage.out
    
    - name: Check coverage
      run: |
        ./scripts/coverage.sh
    
    - name: Upload coverage
      uses: codecov/codecov-action@v4
      with:
        file: ./coverage.out
    
  integration-tests:
    runs-on: ubuntu-latest
    needs: unit-tests
    
    steps:
    - uses: actions/checkout@v4
    
    - uses: actions/setup-go@v5
      with:
        go-version: '1.24.4'
    
    - name: Run integration tests
      run: |
        go test -v ./test/integration/... -tags=integration
    
  benchmarks:
    runs-on: ubuntu-latest
    
    steps:
    - uses: actions/checkout@v4
    
    - uses: actions/setup-go@v5
      with:
        go-version: '1.24.4'
    
    - name: Run benchmarks
      run: |
        go test -bench=. -benchmem ./test/benchmark/... | tee benchmark.txt
    
    - name: Store benchmark result
      uses: benchmark-action/github-action-benchmark@v1
      with:
        tool: 'go'
        output-file-path: benchmark.txt
        github-token: ${{ secrets.GITHUB_TOKEN }}
        auto-push: true
```

## Phase 10 Deliverables

1. ✅ Unit test structure with 85% coverage requirement
2. ✅ Integration tests with mock services
3. ✅ Performance benchmarks for crypto and sync
4. ✅ Test utilities and fixtures
5. ✅ Coverage and mutation testing scripts
6. ✅ CI/CD integration with automated testing
7. ✅ Table-driven tests following Go best practices
8. ✅ Race condition testing

## Verification Commands

```bash
# Run all tests
go test -v ./...

# Run with race detection
go test -race ./...

# Generate coverage report
./scripts/coverage.sh

# Run benchmarks
go test -bench=. ./test/benchmark/...

# Run integration tests
go test -v ./test/integration/... -tags=integration

# Run mutation testing
./scripts/mutation.sh
```

## Next Steps

With comprehensive testing in place, proceed to create [CLAUDE.md](../CLAUDE.md) for AI development guidelines and the troubleshooting guide.