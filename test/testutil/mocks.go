package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/TheMichaelB/obsync/internal/crypto"
	"github.com/TheMichaelB/obsync/internal/models"
	"github.com/TheMichaelB/obsync/internal/transport"
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

func (m *MockCryptoProvider) EncryptData(plaintext, key []byte) ([]byte, error) {
	args := m.Called(plaintext, key)
	if data := args.Get(0); data != nil {
		return data.([]byte), args.Error(1)
	}
	return nil, args.Error(1)
}

// MockStateStore mocks state persistence.
type MockStateStore struct {
	mock.Mock
	mu     sync.RWMutex
	states map[string]*models.SyncState
}

func NewMockStateStore() *MockStateStore {
	return &MockStateStore{
		states: make(map[string]*models.SyncState),
	}
}

func (m *MockStateStore) Load(vaultID string) (*models.SyncState, error) {
	args := m.Called(vaultID)
	
	if state := args.Get(0); state != nil {
		return state.(*models.SyncState), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockStateStore) Save(vaultID string, state *models.SyncState) error {
	args := m.Called(vaultID, state)
	
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[vaultID] = state
	
	return args.Error(0)
}

func (m *MockStateStore) Delete(vaultID string) error {
	args := m.Called(vaultID)
	
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, vaultID)
	
	return args.Error(0)
}

func (m *MockStateStore) List() ([]string, error) {
	args := m.Called()
	
	if vaults := args.Get(0); vaults != nil {
		return vaults.([]string), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockStateStore) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states = make(map[string]*models.SyncState)
}

// MockBlobStore mocks blob storage operations.
type MockBlobStore struct {
	mock.Mock
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMockBlobStore() *MockBlobStore {
	return &MockBlobStore{
		data: make(map[string][]byte),
	}
}

func (m *MockBlobStore) Write(path string, data []byte, mode os.FileMode) error {
	args := m.Called(path, data, mode)
	
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[path] = make([]byte, len(data))
	copy(m.data[path], data)
	
	return args.Error(0)
}

func (m *MockBlobStore) Read(path string) ([]byte, error) {
	args := m.Called(path)
	
	if data := args.Get(0); data != nil {
		return data.([]byte), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockBlobStore) Delete(path string) error {
	args := m.Called(path)
	
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, path)
	
	return args.Error(0)
}

func (m *MockBlobStore) Exists(path string) (bool, error) {
	args := m.Called(path)
	return args.Bool(0), args.Error(1)
}

func (m *MockBlobStore) Size(path string) (int64, error) {
	args := m.Called(path)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockBlobStore) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[string][]byte)
}

// TestTransport provides a test transport implementation.
type TestTransport struct {
	transport.Transport
	messages []models.WSMessage
	delay    time.Duration
}

func NewTestTransport() *TestTransport {
	return &TestTransport{}
}

func (t *TestTransport) SetMessages(messages []models.WSMessage) {
	t.messages = messages
}

func (t *TestTransport) SetDelay(delay time.Duration) {
	t.delay = delay
}

// TestCrypto provides mock crypto implementations for testing.
type TestCrypto struct{}

func NewTestCrypto() *TestCrypto {
	return &TestCrypto{}
}

// TestVaultKey returns a test encryption key.
func TestVaultKey() []byte {
	key := make([]byte, 32)
	copy(key, []byte("test-vault-key-12345678901234567890"))
	return key
}

// DecryptMockPath provides mock path decryption for testing.
func DecryptMockPath(hexPath string) string {
	// Simple mock: reverse hex encoding for test paths
	data, err := hex.DecodeString(hexPath)
	if err != nil {
		return hexPath // Return as-is if not valid hex
	}
	return string(data)
}

// DecryptMockData provides mock data decryption for testing.
func DecryptMockData(ciphertext []byte) []byte {
	// Simple mock: XOR with test pattern
	result := make([]byte, len(ciphertext))
	for i, b := range ciphertext {
		result[i] = b ^ 0xAA
	}
	return result
}

// EncryptMockPath provides mock path encryption for testing.
func EncryptMockPath(path string, key []byte) (string, error) {
	return hex.EncodeToString([]byte(path)), nil
}

// EncryptMockData provides mock data encryption for testing.
func EncryptMockData(plaintext []byte) []byte {
	// Simple mock: XOR with test pattern  
	result := make([]byte, len(plaintext))
	for i, b := range plaintext {
		result[i] = b ^ 0xAA
	}
	return result
}

// WSInitResponse creates test WebSocket init response.
func WSInitResponse(success bool, totalFiles int) models.WSMessage {
	return models.WSMessage{
		Type:      models.WSTypeInitResponse,
		Timestamp: time.Now(),
		Data:      mustMarshal(models.InitResponse{Success: success, TotalFiles: totalFiles}),
	}
}

// WSFileMessage creates test WebSocket file message.
func WSFileMessage(uid int, path, content string) models.WSMessage {
	return models.WSMessage{
		Type:      models.WSTypeFile,
		UID:       uid,
		Timestamp: time.Now(),
		Data: mustMarshal(models.FileMessage{
			UID:     uid,
			Path:    hex.EncodeToString([]byte(path)),
			Hash:    fmt.Sprintf("%x", rand.Reader),
			Size:    int64(len(content)),
			ChunkID: fmt.Sprintf("chunk-%d", uid),
		}),
	}
}

// WSDoneMessage creates test WebSocket done message.
func WSDoneMessage(version, synced int) models.WSMessage {
	return models.WSMessage{
		Type:      models.WSTypeDone,
		Timestamp: time.Now(),
		Data: mustMarshal(models.DoneMessage{
			FinalVersion: version,
			TotalSynced:  synced,
		}),
	}
}

// WSErrorMessage creates test WebSocket error message.
func WSErrorMessage(code, message string) models.WSMessage {
	return models.WSMessage{
		Type:      models.WSTypeError,
		Timestamp: time.Now(),
		Data: mustMarshal(models.ErrorMessage{
			Code:    code,
			Message: message,
			Fatal:   false,
		}),
	}
}

// TestConfig provides standard test configuration.
func TestConfig() map[string]interface{} {
	return map[string]interface{}{
		"api": map[string]interface{}{
			"base_url": "https://api.test.com",
			"timeout":  "30s",
		},
		"storage": map[string]interface{}{
			"data_dir":  "/tmp/obsync-test",
			"state_dir": "/tmp/obsync-test/state",
		},
		"sync": map[string]interface{}{
			"max_concurrent":     5,
			"chunk_size":        1048576,
			"retry_attempts":    3,
			"retry_backoff":     "1s",
		},
		"log": map[string]interface{}{
			"level":  "debug",
			"format": "json",
		},
	}
}

// RandomSalt generates a random salt for testing.
func RandomSalt() string {
	salt := make([]byte, 32)
	rand.Read(salt)
	return hex.EncodeToString(salt)
}

// SampleFiles provides test file content for fixtures.
var SampleFiles = map[string]string{
	"notes/welcome.md": `# Welcome to Obsidian

This is a test vault for development and testing.

## Features
- Markdown support
- Linked notes
- File attachments

![Example](attachments/example.png)
`,
	"daily/2024-01-15.md": `# Daily Note - 2024-01-15

## Tasks
- [x] Review Phase 10 implementation
- [ ] Write integration tests
- [ ] Update documentation

## Notes
Quick note about testing strategies.
`,
	"concepts/testing.md": `# Testing Strategy

## Unit Tests
- Table-driven tests
- Mock dependencies
- 85% coverage target

## Integration Tests  
- Real service integration
- End-to-end scenarios
- Performance benchmarks

Links: [[daily/2024-01-15]]
`,
	"attachments/readme.txt": `This folder contains binary attachments and images.

Files in testing:
- example.png (sample image)
- document.pdf (sample PDF)
`,
}

// SampleBinaryFiles provides test binary content.
var SampleBinaryFiles = map[string][]byte{
	"attachments/example.png": {
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG header
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1 pixel
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x09, 0x70, 0x48, 0x59,
		0x73, 0x00, 0x00, 0x0B, 0x13, 0x00, 0x00, 0x0B,
		0x13, 0x01, 0x00, 0x9A, 0x9C, 0x18, 0x00, 0x00,
		0x00, 0x0C, 0x49, 0x44, 0x41, 0x54, 0x08, 0x57,
		0x63, 0xF8, 0x0F, 0x00, 0x00, 0x01, 0x00, 0x01,
		0x5C, 0x6A, 0xE2, 0x8F, 0x00, 0x00, 0x00, 0x00,
		0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
	},
	"attachments/document.pdf": {
		0x25, 0x50, 0x44, 0x46, 0x2D, 0x31, 0x2E, 0x34, // %PDF-1.4
		0x0A, 0x25, 0xE2, 0xE3, 0xCF, 0xD3, 0x0A, // Binary comment
		// Minimal PDF structure for testing
		0x31, 0x20, 0x30, 0x20, 0x6F, 0x62, 0x6A, 0x0A, // 1 0 obj
		0x3C, 0x3C, 0x2F, 0x54, 0x79, 0x70, 0x65, 0x2F, // <</Type/
		0x43, 0x61, 0x74, 0x61, 0x6C, 0x6F, 0x67, 0x2F, // Catalog/
		0x50, 0x61, 0x67, 0x65, 0x73, 0x20, 0x32, 0x20, // Pages 2 
		0x30, 0x20, 0x52, 0x3E, 0x3E, 0x0A, 0x65, 0x6E, // 0 R>>.en
		0x64, 0x6F, 0x62, 0x6A, 0x0A, // dobj.
	},
}

// AssertMockExpectations verifies all mock expectations.
func AssertMockExpectations(t TestingT, mocks ...interface{}) {
	for _, m := range mocks {
		if mockObj, ok := m.(interface{ AssertExpectations(TestingT) bool }); ok {
			mockObj.AssertExpectations(t)
		}
	}
}

// TestingT is a minimal interface for testing.T compatibility.
type TestingT interface {
	Errorf(format string, args ...interface{})
	FailNow()
}