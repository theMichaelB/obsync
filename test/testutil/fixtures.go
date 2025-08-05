package testutil

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	
	"github.com/yourusername/obsync/internal/events"
	"github.com/yourusername/obsync/internal/models"
)

// NewTestLogger creates a logger for testing.
func NewTestLogger() *events.Logger {
	var buf bytes.Buffer
	return events.NewTestLogger(events.DebugLevel, "json", &buf)
}

// Fixture represents test vault data.
type Fixture struct {
	Vault         *models.Vault
	VaultKey      []byte
	Email         string
	Password      string
	WSTrace       string
	InitialTrace  string
	IncrementalTrace string
	ExpectedFiles []ExpectedFile
}

// ExpectedFile describes expected sync result.
type ExpectedFile struct {
	Path    string
	Content string
	Hash    string
	Size    int64
	Binary  bool
}

// LoadFixture loads test fixture data.
func LoadFixture(name string) *Fixture {
	path := filepath.Join("test", "fixtures", name, "fixture.json")
	
	data, err := os.ReadFile(path)
	if err != nil {
		// Return default fixture if not found
		return defaultFixture(name)
	}
	
	var fixture Fixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		panic(fmt.Errorf("parse fixture %s: %w", name, err))
	}
	
	// Load WebSocket trace
	tracePath := filepath.Join("test", "fixtures", name, "ws_trace.json")
	if traceData, err := os.ReadFile(tracePath); err == nil {
		fixture.WSTrace = string(traceData)
	}
	
	// Load initial trace for incremental testing
	initialPath := filepath.Join("test", "fixtures", name, "initial_trace.json")
	if initialData, err := os.ReadFile(initialPath); err == nil {
		fixture.InitialTrace = string(initialData)
	}
	
	// Load incremental trace
	incrementalPath := filepath.Join("test", "fixtures", name, "incremental_trace.json")
	if incrementalData, err := os.ReadFile(incrementalPath); err == nil {
		fixture.IncrementalTrace = string(incrementalData)
	}
	
	return &fixture
}

// defaultFixture creates a default fixture for testing.
func defaultFixture(name string) *Fixture {
	vault := &models.Vault{
		ID:   fmt.Sprintf("fixture-%s", name),
		Name: fmt.Sprintf("Test Vault (%s)", name),
		EncryptionInfo: models.KeyInfo{
			Version:           1,
			EncryptionVersion: 3,
			Salt:             "dGVzdHNhbHQ=", // "testsalt" in base64
		},
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	
	var expectedFiles []ExpectedFile
	for path, content := range SampleFiles {
		hash := sha256.Sum256([]byte(content))
		expectedFiles = append(expectedFiles, ExpectedFile{
			Path:    path,
			Content: content,
			Hash:    hex.EncodeToString(hash[:]),
			Size:    int64(len(content)),
			Binary:  false,
		})
	}
	
	for path, content := range SampleBinaryFiles {
		hash := sha256.Sum256(content)
		expectedFiles = append(expectedFiles, ExpectedFile{
			Path:    path,
			Content: string(content),
			Hash:    hex.EncodeToString(hash[:]),
			Size:    int64(len(content)),
			Binary:  true,
		})
	}
	
	return &Fixture{
		Vault:         vault,
		VaultKey:      TestVaultKey(),
		Email:         "test@example.com",
		Password:      "testpassword123",
		ExpectedFiles: expectedFiles,
	}
}

// GenerateSyncMessages creates test WebSocket messages.
func GenerateSyncMessages(fileCount int, fileSize int) []models.WSMessage {
	messages := []models.WSMessage{
		{
			Type:      models.WSTypeInitResponse,
			Timestamp: time.Now(),
			Data: mustMarshal(models.InitResponse{
				Success:    true,
				TotalFiles: fileCount,
			}),
		},
	}
	
	for i := 1; i <= fileCount; i++ {
		content := make([]byte, fileSize)
		for j := range content {
			content[j] = byte(i % 256)
		}
		
		hash := sha256.Sum256(content)
		
		messages = append(messages, models.WSMessage{
			Type:      models.WSTypeFile,
			UID:       i,
			Timestamp: time.Now(),
			Data: mustMarshal(models.FileMessage{
				UID:     i,
				Path:    hex.EncodeToString([]byte(fmt.Sprintf("file%d.dat", i))),
				Hash:    hex.EncodeToString(hash[:]),
				Size:    int64(fileSize),
				ChunkID: fmt.Sprintf("chunk-%d", i),
			}),
		})
	}
	
	messages = append(messages, models.WSMessage{
		Type:      models.WSTypeDone,
		Timestamp: time.Now(),
		Data: mustMarshal(models.DoneMessage{
			FinalVersion: fileCount,
			TotalSynced:  fileCount,
		}),
	})
	
	return messages
}

// CreateTestFixture saves a fixture to disk for integration tests.
func CreateTestFixture(name string, fixture *Fixture) error {
	fixtureDir := filepath.Join("test", "fixtures", name)
	if err := os.MkdirAll(fixtureDir, 0755); err != nil {
		return fmt.Errorf("create fixture dir: %w", err)
	}
	
	// Save fixture metadata
	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal fixture: %w", err)
	}
	
	fixturePath := filepath.Join(fixtureDir, "fixture.json")
	if err := os.WriteFile(fixturePath, data, 0644); err != nil {
		return fmt.Errorf("write fixture: %w", err)
	}
	
	// Save WebSocket trace if present
	if fixture.WSTrace != "" {
		tracePath := filepath.Join(fixtureDir, "ws_trace.json")
		if err := os.WriteFile(tracePath, []byte(fixture.WSTrace), 0644); err != nil {
			return fmt.Errorf("write trace: %w", err)
		}
	}
	
	return nil
}

// mustMarshal marshals data to JSON or panics.
func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return json.RawMessage(data)
}

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