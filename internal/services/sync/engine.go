package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yourusername/obsync/internal/crypto"
	"github.com/yourusername/obsync/internal/events"
	"github.com/yourusername/obsync/internal/models"
	"github.com/yourusername/obsync/internal/state"
	"github.com/yourusername/obsync/internal/storage"
	"github.com/yourusername/obsync/internal/transport"
)

// Engine implements the sync algorithm.
type Engine struct {
	transport transport.Transport
	crypto    crypto.Provider
	state     state.Store
	storage   storage.BlobStore
	logger    *events.Logger

	// Configuration
	maxConcurrent int
	chunkSize     int

	// Progress tracking
	progress atomic.Value // *Progress
	events   chan Event

	// Sync state
	mu          sync.Mutex
	syncing     bool
	cancelFn    context.CancelFunc
	eventsClosed bool
}

// Progress tracks sync progress.
type Progress struct {
	Phase           string
	TotalFiles      int
	ProcessedFiles  int
	CurrentFile     string
	BytesDownloaded int64
	StartTime       time.Time
	Errors          []error
}

// Event represents a sync event.
type Event struct {
	Type      EventType
	Timestamp time.Time
	File      *models.FileItem
	Error     error
	Progress  *Progress
}

// EventType defines sync event types.
type EventType string

const (
	EventStarted      EventType = "started"
	EventFileStarted  EventType = "file_started"
	EventFileComplete EventType = "file_complete"
	EventFileSkipped  EventType = "file_skipped"
	EventFileError    EventType = "file_error"
	EventProgress     EventType = "progress"
	EventCompleted    EventType = "completed"
	EventFailed       EventType = "failed"
)

// SyncConfig contains sync configuration.
type SyncConfig struct {
	MaxConcurrent int
	ChunkSize     int
}

// NewEngine creates a sync engine.
func NewEngine(
	transport transport.Transport,
	crypto crypto.Provider,
	state state.Store,
	storage storage.BlobStore,
	config *SyncConfig,
	logger *events.Logger,
) *Engine {
	return &Engine{
		transport:     transport,
		crypto:        crypto,
		state:         state,
		storage:       storage,
		logger:        logger.WithField("component", "sync_engine"),
		maxConcurrent: config.MaxConcurrent,
		chunkSize:     config.ChunkSize,
		events:        make(chan Event, 100),
		eventsClosed:  false,
	}
}

// Events returns the event channel.
func (e *Engine) Events() <-chan Event {
	return e.events
}

// GetProgress returns current progress.
func (e *Engine) GetProgress() *Progress {
	if p := e.progress.Load(); p != nil {
		return p.(*Progress)
	}
	return nil
}

// Sync performs a vault synchronization.
func (e *Engine) Sync(ctx context.Context, vaultID, vaultHost string, vaultKey []byte, encryptionVersion int, initial bool) error {
	e.mu.Lock()
	if e.syncing {
		e.mu.Unlock()
		return models.ErrSyncInProgress
	}
	e.syncing = true
	
	// Create new events channel if previous was closed
	if e.eventsClosed {
		e.events = make(chan Event, 100)
		e.eventsClosed = false
	}

	ctx, cancel := context.WithCancel(ctx)
	e.cancelFn = cancel
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.syncing = false
		e.cancelFn = nil
		if !e.eventsClosed {
			close(e.events)
			e.eventsClosed = true
		}
		e.mu.Unlock()
	}()

	// Initialize progress
	progress := &Progress{
		Phase:     "initializing",
		StartTime: time.Now(),
	}
	e.progress.Store(progress)

	e.logger.WithFields(map[string]interface{}{
		"vault_id": vaultID,
		"initial":  initial,
	}).Info("Starting sync")

	// Emit start event
	e.emitEvent(Event{
		Type:      EventStarted,
		Timestamp: time.Now(),
		Progress:  progress,
	})

	// Load or create state
	syncState, err := e.loadOrCreateState(vaultID)
	if err != nil {
		return e.handleError(fmt.Errorf("load state: %w", err))
	}

	// Determine sync parameters
	if initial {
		syncState.Version = 0
		syncState.Files = make(map[string]string)
	}

	// Generate keyhash (SHA256 of vault key)
	hasher := sha256.New()
	hasher.Write(vaultKey)
	keyhash := hex.EncodeToString(hasher.Sum(nil))

	// Connect to WebSocket
	initMsg := models.InitMessage{
		Op:                "init",
		Token:             e.transport.GetToken(),
		ID:                vaultID,
		Keyhash:           keyhash,
		Version:           syncState.Version,
		Initial:           initial,
		Device:            "ObsyncGo",
		EncryptionVersion: encryptionVersion,
	}

	msgChan, err := e.transport.StreamWS(ctx, vaultHost, initMsg)
	if err != nil {
		return e.handleError(fmt.Errorf("connect websocket: %w", err))
	}

	// Process messages
	// We'll decrypt paths directly using the crypto provider

	messageCount := 0
	for msg := range msgChan {
		messageCount++
		e.logger.WithFields(map[string]interface{}{
			"msg_type": msg.Type,
			"msg_uid":  msg.UID,
			"count":    messageCount,
		}).Debug("Processing message")
		
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := e.processMessage(ctx, msg, vaultKey, syncState); err != nil {
			e.logger.WithError(err).WithField("msg_type", msg.Type).Error("Failed to process message")
			progress.Errors = append(progress.Errors, err)

			// Determine if error is fatal
			if e.isFatalError(err) {
				return e.handleError(err)
			}
		}
	}
	
	e.logger.WithField("total_messages", messageCount).Debug("Finished processing messages")

	// Save final state
	finalizeProgress := *progress
	finalizeProgress.Phase = "finalizing"
	e.progress.Store(&finalizeProgress)
	if err := e.state.Save(vaultID, syncState); err != nil {
		return e.handleError(fmt.Errorf("save state: %w", err))
	}

	// Emit completion
	completedProgress := finalizeProgress
	completedProgress.Phase = "completed"
	e.progress.Store(&completedProgress)
	e.emitEvent(Event{
		Type:      EventCompleted,
		Timestamp: time.Now(),
		Progress:  &completedProgress,
	})

	e.logger.WithFields(map[string]interface{}{
		"duration": time.Since(completedProgress.StartTime),
		"files":    completedProgress.ProcessedFiles,
		"errors":   len(completedProgress.Errors),
	}).Info("Sync completed")

	return nil
}

// Cancel stops an ongoing sync.
func (e *Engine) Cancel() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cancelFn != nil {
		e.logger.Info("Cancelling sync")
		e.cancelFn()
	}
}

// processMessage handles a single WebSocket message.
func (e *Engine) processMessage(
	ctx context.Context,
	msg models.WSMessage,
	vaultKey []byte,
	syncState *models.SyncState,
) error {
	switch msg.Type {
	case models.WSTypeInitResponse:
		return e.handleInitResponse(msg)

	case models.WSTypeFile:
		return e.handleFileMessage(ctx, msg, vaultKey, syncState)

	case models.WSTypeFolder:
		return e.handleFolderMessage(msg, vaultKey)

	case models.WSTypeDelete:
		return e.handleDeleteMessage(msg, vaultKey, syncState)

	case models.WSTypeDone:
		return e.handleDoneMessage(msg, syncState)

	case models.WSTypeError:
		return e.handleErrorMessage(msg)

	default:
		e.logger.WithField("type", msg.Type).Debug("Ignoring message type")
		return nil
	}
}

// handleFileMessage processes a file sync message.
func (e *Engine) handleFileMessage(
	ctx context.Context,
	msg models.WSMessage,
	vaultKey []byte,
	syncState *models.SyncState,
) error {
	var fileMsg models.FileMessage
	if err := json.Unmarshal(msg.Data, &fileMsg); err != nil {
		return fmt.Errorf("parse file message: %w", err)
	}

	// Decrypt path
	plainPath, err := e.crypto.DecryptPath(fileMsg.Path, vaultKey)
	if err != nil {
		return &models.DecryptError{
			Path:   fileMsg.Path,
			Reason: "path decryption",
			Err:    err,
		}
	}

	progress := e.GetProgress()
	if progress != nil {
		updatedProgress := *progress
		updatedProgress.CurrentFile = plainPath
		e.progress.Store(&updatedProgress)
		progress = &updatedProgress
	}

	e.logger.WithFields(map[string]interface{}{
		"uid":  fileMsg.UID,
		"path": plainPath,
		"size": fileMsg.Size,
	}).Debug("Processing file")

	// Check if file needs update
	existingHash, exists := syncState.Files[plainPath]
	if exists && existingHash == fileMsg.Hash {
		e.logger.Debug("File unchanged, skipping")
		e.emitEvent(Event{
			Type:      EventFileSkipped,
			Timestamp: time.Now(),
			File: &models.FileItem{
				Path: plainPath,
				Hash: fileMsg.Hash,
			},
		})
		syncState.Version = fileMsg.UID
		return nil
	}

	// Emit start event
	e.emitEvent(Event{
		Type:      EventFileStarted,
		Timestamp: time.Now(),
		File: &models.FileItem{
			Path: plainPath,
			Hash: fileMsg.Hash,
			Size: fileMsg.Size,
		},
	})

	// Download and decrypt chunk
	if fileMsg.ChunkID != "" {
		encryptedData, err := e.transport.DownloadChunk(ctx, fileMsg.ChunkID)
		if err != nil {
			return fmt.Errorf("download chunk %s: %w", fileMsg.ChunkID, err)
		}

		// Decrypt content
		plainData, err := e.crypto.DecryptData(encryptedData, vaultKey)
		if err != nil {
			return &models.DecryptError{
				Path:   plainPath,
				Reason: "content decryption",
				Err:    err,
			}
		}

		// Verify hash
		hash := sha256.Sum256(plainData)
		actualHash := hex.EncodeToString(hash[:])
		if actualHash != fileMsg.Hash {
			return &models.IntegrityError{
				Path:     plainPath,
				Expected: fileMsg.Hash,
				Actual:   actualHash,
			}
		}

		// Detect if binary
		isBinary := models.IsBinaryFile(plainPath, plainData)

		// Write to storage
		mode := os.FileMode(0644)
		if isBinary {
			mode = 0644
		}

		if err := e.storage.Write(plainPath, plainData, mode); err != nil {
			return fmt.Errorf("write file: %w", err)
		}

		// Set modification time
		if !fileMsg.ModifiedTime.IsZero() {
			_ = e.storage.SetModTime(plainPath, fileMsg.ModifiedTime)
		}

		// Update progress
		atomic.AddInt64(&progress.BytesDownloaded, int64(len(plainData)))
	}

	// Update state
	syncState.Files[plainPath] = fileMsg.Hash
	syncState.Version = fileMsg.UID

	// Emit complete event
	if progress != nil {
		updatedProgress := *progress
		updatedProgress.ProcessedFiles++
		e.progress.Store(&updatedProgress)
		progress = &updatedProgress
	}
	e.emitEvent(Event{
		Type:      EventFileComplete,
		Timestamp: time.Now(),
		File: &models.FileItem{
			Path: plainPath,
			Hash: fileMsg.Hash,
		},
		Progress: progress,
	})

	return nil
}

// Additional handler methods...

func (e *Engine) handleInitResponse(msg models.WSMessage) error {
	var resp models.InitResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return fmt.Errorf("parse init response: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("sync initialization failed: %s", resp.Message)
	}

	progress := e.GetProgress()
	if progress != nil {
		updatedProgress := *progress
		updatedProgress.Phase = "syncing"
		updatedProgress.TotalFiles = resp.TotalFiles
		e.progress.Store(&updatedProgress)
	}

	e.logger.WithFields(map[string]interface{}{
		"total_files":    resp.TotalFiles,
		"start_version":  resp.StartVersion,
		"latest_version": resp.LatestVersion,
	}).Info("Sync initialized")

	return nil
}

func (e *Engine) handleFolderMessage(msg models.WSMessage, vaultKey []byte) error {
	var folderMsg models.FolderMessage
	if err := json.Unmarshal(msg.Data, &folderMsg); err != nil {
		return fmt.Errorf("parse folder message: %w", err)
	}

	// Decrypt path
	plainPath, err := e.crypto.DecryptPath(folderMsg.Path, vaultKey)
	if err != nil {
		return fmt.Errorf("decrypt folder path: %w", err)
	}

	// Create directory
	return e.storage.EnsureDir(plainPath)
}

func (e *Engine) handleDeleteMessage(msg models.WSMessage, vaultKey []byte, syncState *models.SyncState) error {
	var deleteMsg models.DeleteMessage
	if err := json.Unmarshal(msg.Data, &deleteMsg); err != nil {
		return fmt.Errorf("parse delete message: %w", err)
	}

	// Decrypt path
	plainPath, err := e.crypto.DecryptPath(deleteMsg.Path, vaultKey)
	if err != nil {
		return fmt.Errorf("decrypt delete path: %w", err)
	}

	e.logger.WithField("path", plainPath).Info("Deleting file")

	// Delete from storage
	if err := e.storage.Delete(plainPath); err != nil {
		e.logger.WithError(err).Warn("Failed to delete file")
	}

	// Update state
	delete(syncState.Files, plainPath)
	syncState.Version = deleteMsg.UID

	return nil
}

func (e *Engine) handleDoneMessage(msg models.WSMessage, syncState *models.SyncState) error {
	var doneMsg models.DoneMessage
	if err := json.Unmarshal(msg.Data, &doneMsg); err != nil {
		return fmt.Errorf("parse done message: %w", err)
	}

	syncState.Version = doneMsg.FinalVersion
	syncState.LastSyncTime = time.Now()

	e.logger.WithFields(map[string]interface{}{
		"final_version": doneMsg.FinalVersion,
		"total_synced":  doneMsg.TotalSynced,
		"duration":      doneMsg.Duration,
	}).Info("Sync stream completed")

	return nil
}

func (e *Engine) handleErrorMessage(msg models.WSMessage) error {
	var errMsg models.ErrorMessage
	if err := json.Unmarshal(msg.Data, &errMsg); err != nil {
		return fmt.Errorf("parse error message: %w", err)
	}

	e.logger.WithFields(map[string]interface{}{
		"code":  errMsg.Code,
		"fatal": errMsg.Fatal,
		"path":  errMsg.Path,
	}).Error(errMsg.Message)

	if errMsg.Fatal {
		return fmt.Errorf("server error [%s]: %s", errMsg.Code, errMsg.Message)
	}

	return nil
}

// Helper methods

func (e *Engine) loadOrCreateState(vaultID string) (*models.SyncState, error) {
	syncState, err := e.state.Load(vaultID)
	if err == nil {
		return syncState, nil
	}

	if errors.Is(err, state.ErrStateNotFound) {
		// Create new state
		return models.NewSyncState(vaultID), nil
	}

	return nil, err
}

func (e *Engine) emitEvent(event Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.eventsClosed {
		return
	}
	
	select {
	case e.events <- event:
	default:
		// Channel full, drop event
		e.logger.Debug("Event channel full, dropping event")
	}
}

func (e *Engine) handleError(err error) error {
	e.emitEvent(Event{
		Type:      EventFailed,
		Timestamp: time.Now(),
		Error:     err,
	})
	return err
}

func (e *Engine) isFatalError(err error) bool {
	// Determine which errors should stop sync
	switch {
	case errors.Is(err, context.Canceled):
		return true
	case errors.Is(err, models.ErrDecryptionFailed):
		return true
	case strings.Contains(err.Error(), "server error"):
		return true
	default:
		return false
	}
}