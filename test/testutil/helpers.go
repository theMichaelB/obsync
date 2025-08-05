package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/obsync/internal/config"
	"github.com/yourusername/obsync/internal/models"
)

// LogEntry represents a captured log entry for testing
type LogEntry struct {
	Level   string                 `json:"level"`
	Message string                 `json:"message"`
	Time    time.Time              `json:"time"`
	Fields  map[string]interface{} `json:"fields,omitempty"`
}

// TestServer provides a mock HTTP server for integration tests.
type TestServer struct {
	*httptest.Server
	mu           sync.RWMutex
	vaults       map[string]*models.Vault
	vaultKeys    map[string][]byte
	wsTraces     map[string]string
	authTokens   map[string]string
	loginHandler func(email, password string) (string, error)
}

// NewTestServer creates a new test HTTP server.
func NewTestServer() *TestServer {
	ts := &TestServer{
		vaults:     make(map[string]*models.Vault),
		vaultKeys:  make(map[string][]byte),
		wsTraces:   make(map[string]string),
		authTokens: make(map[string]string),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/login", ts.handleLogin)
	mux.HandleFunc("/api/vaults", ts.handleVaults)
	mux.HandleFunc("/api/sync/", ts.handleSync)

	ts.Server = httptest.NewServer(mux)
	return ts
}

// AddVault adds a vault to the test server.
func (ts *TestServer) AddVault(vault *models.Vault) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.vaults[vault.ID] = vault
}

// SetVaultKey sets the encryption key for a vault.
func (ts *TestServer) SetVaultKey(vaultID string, key []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.vaultKeys[vaultID] = key
}

// LoadWSTrace loads a WebSocket trace for a vault.
func (ts *TestServer) LoadWSTrace(vaultID, trace string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.wsTraces[vaultID] = trace
}

// SetLoginHandler sets a custom login handler.
func (ts *TestServer) SetLoginHandler(handler func(email, password string) (string, error)) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.loginHandler = handler
}

func (ts *TestServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var loginReq struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := decodeJSON(r.Body, &loginReq); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	ts.mu.RLock()
	handler := ts.loginHandler
	ts.mu.RUnlock()

	var token string
	var err error

	if handler != nil {
		token, err = handler(loginReq.Email, loginReq.Password)
	} else {
		// Default test authentication
		if loginReq.Email == "test@example.com" && loginReq.Password == "testpassword123" {
			token = "test-token-12345"
		} else {
			err = fmt.Errorf("invalid credentials")
		}
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	ts.mu.Lock()
	ts.authTokens[token] = loginReq.Email
	ts.mu.Unlock()

	response := map[string]interface{}{
		"success": true,
		"token":   token,
		"user": map[string]interface{}{
			"email": loginReq.Email,
		},
	}

	writeJSON(w, response)
}

func (ts *TestServer) handleVaults(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if token == "" || !ts.isValidToken(token) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ts.mu.RLock()
	var vaults []*models.Vault
	for _, vault := range ts.vaults {
		vaults = append(vaults, vault)
	}
	ts.mu.RUnlock()

	writeJSON(w, map[string]interface{}{
		"vaults": vaults,
	})
}

func (ts *TestServer) handleSync(w http.ResponseWriter, r *http.Request) {
	// WebSocket sync endpoint - simplified for testing
	token := r.Header.Get("Authorization")
	if token == "" || !ts.isValidToken(token) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// For testing, return success
	writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Sync endpoint available",
	})
}

func (ts *TestServer) isValidToken(token string) bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	_, exists := ts.authTokens[token]
	return exists
}

// TestHelpers provides common test helper functions.
type TestHelpers struct {
	t       *testing.T
	tempDir string
	cleanup []func()
}

// NewTestHelpers creates test helpers.
func NewTestHelpers(t *testing.T) *TestHelpers {
	tempDir := t.TempDir()
	return &TestHelpers{
		t:       t,
		tempDir: tempDir,
	}
}

// TempDir returns the temporary directory for this test.
func (h *TestHelpers) TempDir() string {
	return h.tempDir
}

// CreateTempFile creates a temporary file with content.
func (h *TestHelpers) CreateTempFile(name, content string) string {
	path := filepath.Join(h.tempDir, name)
	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, 0755)
	require.NoError(h.t, err)

	err = os.WriteFile(path, []byte(content), 0644)
	require.NoError(h.t, err)

	return path
}

// CreateTempBinaryFile creates a temporary binary file.
func (h *TestHelpers) CreateTempBinaryFile(name string, content []byte) string {
	path := filepath.Join(h.tempDir, name)
	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, 0755)
	require.NoError(h.t, err)

	err = os.WriteFile(path, content, 0644)
	require.NoError(h.t, err)

	return path
}

// AssertFileExists checks that a file exists with expected content.
func (h *TestHelpers) AssertFileExists(path string) {
	_, err := os.Stat(path)
	assert.NoError(h.t, err, "File should exist: %s", path)
}

// AssertFileContent checks file content matches expected.
func (h *TestHelpers) AssertFileContent(path, expectedContent string) {
	content, err := os.ReadFile(path)
	require.NoError(h.t, err)
	assert.Equal(h.t, expectedContent, string(content))
}

// AssertFileNotExists checks that a file does not exist.
func (h *TestHelpers) AssertFileNotExists(path string) {
	_, err := os.Stat(path)
	assert.True(h.t, os.IsNotExist(err), "File should not exist: %s", path)
}

// AddCleanup adds a cleanup function to be called at test end.
func (h *TestHelpers) AddCleanup(fn func()) {
	h.cleanup = append(h.cleanup, fn)
}

// Cleanup runs all cleanup functions.
func (h *TestHelpers) Cleanup() {
	for i := len(h.cleanup) - 1; i >= 0; i-- {
		h.cleanup[i]()
	}
}

// TestTimeout provides timeout context for tests.
func TestTimeout(duration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), duration)
}

// TestContext creates a test context with reasonable timeout.
func TestContext() (context.Context, context.CancelFunc) {
	return TestTimeout(30 * time.Second)
}

// TestConfig creates a test configuration.
func TestConfigWithDir(dataDir string) *config.Config {
	return &config.Config{
		API: config.APIConfig{
			BaseURL: "https://api.test.com",
			Timeout: 30 * time.Second,
		},
		Storage: config.StorageConfig{
			DataDir:  dataDir,
			StateDir: filepath.Join(dataDir, "state"),
		},
		Sync: config.SyncConfig{
			MaxConcurrent: 5,
			ChunkSize:     1024 * 1024,
			RetryAttempts: 3,
			RetryDelay:    time.Second,
		},
		Log: config.LogConfig{
			Level:  "debug",
			Format: "json",
			Color:  false,
		},
	}
}

// WaitForCondition waits for a condition to be true with timeout.
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			t.Fatalf("Timeout waiting for condition: %s", message)
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

// CompareFiles compares two files for equality.
func CompareFiles(t *testing.T, path1, path2 string) {
	content1, err := os.ReadFile(path1)
	require.NoError(t, err, "Failed to read %s", path1)

	content2, err := os.ReadFile(path2)
	require.NoError(t, err, "Failed to read %s", path2)

	assert.Equal(t, content1, content2, "Files should be identical")
}

// LogOutput captures log output for testing.
type LogOutput struct {
	mu      sync.RWMutex
	entries []LogEntry
}

// NewLogOutput creates a new log output capturer.
func NewLogOutput() *LogOutput {
	return &LogOutput{}
}

// Write implements io.Writer to capture log output.
func (lo *LogOutput) Write(p []byte) (n int, err error) {
	// Parse log entry from JSON
	var entry LogEntry
	if err := json.Unmarshal(p, &entry); err == nil {
		lo.mu.Lock()
		lo.entries = append(lo.entries, entry)
		lo.mu.Unlock()
	}
	return len(p), nil
}

// Entries returns captured log entries.
func (lo *LogOutput) Entries() []LogEntry {
	lo.mu.RLock()
	defer lo.mu.RUnlock()
	
	entries := make([]LogEntry, len(lo.entries))
	copy(entries, lo.entries)
	return entries
}

// HasLevel checks if any log entry has the specified level.
func (lo *LogOutput) HasLevel(level string) bool {
	lo.mu.RLock()
	defer lo.mu.RUnlock()
	
	for _, entry := range lo.entries {
		if entry.Level == level {
			return true
		}
	}
	return false
}

// HasMessage checks if any log entry contains the message.
func (lo *LogOutput) HasMessage(message string) bool {
	lo.mu.RLock()
	defer lo.mu.RUnlock()
	
	for _, entry := range lo.entries {
		if contains(entry.Message, message) {
			return true
		}
	}
	return false
}

// Clear clears all captured entries.
func (lo *LogOutput) Clear() {
	lo.mu.Lock()
	defer lo.mu.Unlock()
	lo.entries = nil
}

// utility functions
func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}

func writeJSON(w http.ResponseWriter, v interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s[:len(substr)] == substr || 
		len(s) > len(substr) && contains(s[1:], substr))
}

// SkipIfShort skips test if testing.Short() is true.
func SkipIfShort(t *testing.T, reason string) {
	if testing.Short() {
		t.Skipf("Skipping test in short mode: %s", reason)
	}
}