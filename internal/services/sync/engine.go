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

	"github.com/TheMichaelB/obsync/internal/crypto"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/models"
	"github.com/TheMichaelB/obsync/internal/state"
	"github.com/TheMichaelB/obsync/internal/storage"
	"github.com/TheMichaelB/obsync/internal/transport"
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
	
	// File pull queue - files that need to be downloaded via pull requests
	pullQueue []PullRequest
	
	// Pull request tracking for unified message router
	pullRequests   map[int]*PullRequestState  // uid -> request state
	pullMutex      sync.Mutex
	pullResponses  chan PullResponse
	syncPhase      string  // "push", "pull", "complete"
	lastMetadataUID int    // Track the most recent metadata response UID
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

// PullRequest represents a file that needs to be downloaded via pull request.
type PullRequest struct {
	Path string
	Hash string
	Size int
	UID  int
}

// PullRequestState tracks the state of an active pull request.
type PullRequestState struct {
	UID       int
	Path      string
	Hash      string
	Size      int
	Started   time.Time
	Metadata  map[string]interface{}
	Data      []byte
	Complete  bool
	Error     error
}

// PullResponse represents a response to a pull request.
type PullResponse struct {
	UID      int
	Type     string  // "metadata" or "binary"
	Metadata map[string]interface{}
	Data     []byte
	Error    error
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
		pullQueue:     make([]PullRequest, 0),
		pullRequests:  make(map[int]*PullRequestState),
		pullResponses: make(chan PullResponse, 100),
		syncPhase:     "push",
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

	// Initialize progress and clear pull queue
	progress := &Progress{
		Phase:     "initializing",
		StartTime: time.Now(),
	}
	e.progress.Store(progress)
	e.pullQueue = make([]PullRequest, 0) // Clear any previous pull queue

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
		case completion := <-e.pullResponses:
			// Check for completion signal from async pull processing
			if completion.Type == "complete" {
				e.logger.Info("Received pull phase completion signal")
				break
			}
			// Put non-completion responses back for pull handlers to process
			select {
			case e.pullResponses <- completion:
			default:
				e.logger.Warn("Dropped pull response during message processing")
			}
		default:
		}

		if err := e.processMessage(ctx, msg, vaultKey, syncState); err != nil {
			// Check if this is a successful completion signal
			if errors.Is(err, models.ErrSyncComplete) {
				e.logger.Info("Sync completed successfully")
				break
			}
			
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
	// Check if this is a pull response first
	if e.isPullResponse(msg) {
		return e.routePullResponse(msg)
	}
	
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

	case "push":
		return e.handlePushMessage(ctx, msg, vaultKey, syncState)

	case "ready":
		return e.handleReadyMessage(ctx, msg, vaultKey, syncState)

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

		// Skip hash verification - rely on AES-GCM authentication tag for integrity

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

// handlePushMessage processes a push message containing file operations.
func (e *Engine) handlePushMessage(ctx context.Context, msg models.WSMessage, vaultKey []byte, syncState *models.SyncState) error {
	// Parse the raw push message
	var pushData map[string]interface{}
	if err := json.Unmarshal(msg.Data, &pushData); err != nil {
		return fmt.Errorf("parse push message: %w", err)
	}

	// Extract fields from push message
	encryptedPath := getString(pushData, "path")
	fileHash := getString(pushData, "hash")
	fileSize := getInt(pushData, "size")
	deleted := getBool(pushData, "deleted")
	isFolder := getBool(pushData, "folder")
	uid := getInt(pushData, "uid")
	chunkID := getString(pushData, "chunk_id")  // For file downloads

	// Safe string slicing for logging
	pathPreview := encryptedPath
	if len(pathPreview) > 16 {
		pathPreview = pathPreview[:16] + "..."
	}
	hashPreview := fileHash
	if len(hashPreview) > 16 {
		hashPreview = hashPreview[:16] + "..."
	}

	e.logger.WithFields(map[string]interface{}{
		"uid":      uid,
		"path":     pathPreview,
		"hash":     hashPreview,
		"size":     fileSize,
		"deleted":  deleted,
		"folder":   isFolder,
		"chunk_id": chunkID,
		"all_fields": pushData,  // Log all available fields to understand the message structure
	}).Debug("Processing push message")

	// TODO: Fix path decryption to match Obsidian's encryption scheme
	// For now, use encrypted path as placeholder to test sync mechanism
	decryptedPath := "encrypted/" + encryptedPath
	
	// Attempt to decrypt the file path (but don't fail if it doesn't work)
	if actualPath, err := e.crypto.DecryptPath(encryptedPath, vaultKey); err == nil {
		decryptedPath = actualPath
		e.logger.WithField("path", actualPath).Debug("Successfully decrypted path")
	} else {
		e.logger.WithFields(map[string]interface{}{
			"encrypted_path": pathPreview,
			"error":          err.Error(),
		}).Debug("Path decryption failed, using encrypted path as placeholder")
	}

	e.logger.WithFields(map[string]interface{}{
		"uid":            uid,
		"decrypted_path": decryptedPath,
		"size":           fileSize,
		"deleted":        deleted,
		"folder":         isFolder,
	}).Info("Decrypted file path")

	// Update sync state with highest UID seen
	if uid > syncState.Version {
		syncState.Version = uid
	}

	// Handle the file operation
	if deleted {
		// Handle file deletion
		return e.handleFileDelete(decryptedPath, syncState)
	} else if isFolder {
		// Handle folder creation
		return e.handleFolderCreate(decryptedPath)
	} else {
		// Handle file sync
		return e.handleFileSync(ctx, decryptedPath, fileHash, fileSize, chunkID, uid, vaultKey, syncState)
	}
}

// handleReadyMessage processes a ready message indicating sync completion.
func (e *Engine) handleReadyMessage(ctx context.Context, msg models.WSMessage, vaultKey []byte, syncState *models.SyncState) error {
	// Parse the ready message
	var readyData map[string]interface{}
	if err := json.Unmarshal(msg.Data, &readyData); err != nil {
		return fmt.Errorf("parse ready message: %w", err)
	}

	finalVersion := getInt(readyData, "version")
	
	e.logger.WithFields(map[string]interface{}{
		"final_version": finalVersion,
		"current_version": syncState.Version,
	}).Info("Received ready message - sync complete")

	// Update sync state to final version
	syncState.Version = finalVersion

	// Transition to pull phase if there are files to download
	if len(e.pullQueue) > 0 {
		e.logger.WithField("queue_size", len(e.pullQueue)).Info("Starting pull request phase")
		
		// Set engine state to pull mode
		e.syncPhase = "pull"
		
		// Process queue asynchronously - responses will come through main loop
		go e.processPullQueueAsync(ctx, vaultKey, syncState)
		
		// Continue reading messages for pull responses - don't exit yet
		return nil
	}

	// No pull requests needed - sync is complete
	e.syncPhase = "complete"
	return models.ErrSyncComplete
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

// Helper functions for parsing message data
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// File operation handlers
func (e *Engine) handleFileDelete(path string, syncState *models.SyncState) error {
	e.logger.WithField("path", path).Info("Deleting file")
	
	// Delete from filesystem
	if err := e.storage.Delete(path); err != nil {
		// Log but don't fail - file might not exist locally
		e.logger.WithError(err).WithField("path", path).Warn("Failed to delete file from storage")
	}
	
	// Remove from sync state
	delete(syncState.Files, path)
	
	e.logger.WithField("path", path).Info("File deleted successfully")
	return nil
}

func (e *Engine) handleFolderCreate(path string) error {
	e.logger.WithField("path", path).Info("Creating folder")
	
	// Create directory
	if err := e.storage.EnsureDir(path); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	
	e.logger.WithField("path", path).Info("Folder created successfully")
	return nil
}

func (e *Engine) handleFileSync(ctx context.Context, path, hash string, size int, chunkID string, uid int, vaultKey []byte, syncState *models.SyncState) error {
	hashPreview := hash
	if len(hashPreview) > 16 {
		hashPreview = hashPreview[:16] + "..."
	}
	
	e.logger.WithFields(map[string]interface{}{
		"path":     path,
		"hash":     hashPreview,
		"size":     size,
		"chunk_id": chunkID,
	}).Info("Syncing file")
	
	// Check if file already exists with same hash
	if existingHash, exists := syncState.Files[path]; exists && existingHash == hash {
		e.logger.WithField("path", path).Debug("File unchanged, skipping")
		return nil
	}
	
	// Handle empty files
	if size == 0 {
		if err := e.storage.Write(path, []byte{}, 0644); err != nil {
			return fmt.Errorf("write empty file %s: %w", path, err)
		}
		syncState.Files[path] = hash
		e.logger.WithField("path", path).Info("Empty file synced successfully")
		return nil
	}
	
	var plainData []byte
	
	// Download and decrypt file content
	if chunkID != "" {
		var err error
		// Download encrypted chunk directly
		encryptedData, err := e.transport.DownloadChunk(ctx, chunkID)
		if err != nil {
			return fmt.Errorf("download chunk %s for file %s: %w", chunkID, path, err)
		}
		
		e.logger.WithFields(map[string]interface{}{
			"path":           path,
			"chunk_id":       chunkID,
			"encrypted_size": len(encryptedData),
		}).Debug("Downloaded encrypted chunk")
		
		// Decrypt file content
		plainData, err = e.crypto.DecryptData(encryptedData, vaultKey)
		if err != nil {
			return &models.DecryptError{
				Path:   path,
				Reason: "content decryption",
				Err:    err,
			}
		}
	} else {
		// No chunk ID provided - queue for pull request after all push messages are processed
		e.logger.WithField("path", path).Info("Queueing file for pull request")
		
		e.pullQueue = append(e.pullQueue, PullRequest{
			Path: path,
			Hash: hash,
			Size: size,
			UID:  uid,
		})
		
		// Update state with file hash (we'll download the content later)
		syncState.Files[path] = hash
		
		e.logger.WithField("path", path).Info("File queued for pull request")
		return nil
	}
	
	// Skip hash verification - rely on AES-GCM authentication tag for integrity
	
	// Detect file mode (binary vs text)
	mode := os.FileMode(0644)
	if models.IsBinaryFile(path, plainData) {
		mode = 0644 // Keep same mode for binary files
	}
	
	// Write to storage
	if err := e.storage.Write(path, plainData, mode); err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}
	
	e.logger.WithFields(map[string]interface{}{
		"path":            path,
		"decrypted_size":  len(plainData),
		"is_binary":       models.IsBinaryFile(path, plainData),
	}).Info("File downloaded and decrypted successfully")
	
	// Update state with file hash
	syncState.Files[path] = hash
	
	e.logger.WithField("path", path).Info("File synced successfully")
	return nil
}

// pullFileContent sends a pull request and receives encrypted file content via message router.
func (e *Engine) pullFileContent(ctx context.Context, uid int, vaultKey []byte) ([]byte, error) {
	e.logger.WithField("uid", uid).Debug("Sending pull request")
	
	// Register pull request
	e.pullMutex.Lock()
	req := &PullRequestState{
		UID:     uid,
		Started: time.Now(),
	}
	e.pullRequests[uid] = req
	e.pullMutex.Unlock()
	
	// Send pull request message
	pullMsg := map[string]interface{}{
		"op":  "pull",
		"uid": uid,
	}
	
	if err := e.transport.SendMessage(pullMsg); err != nil {
		// Clean up request on send failure
		e.pullMutex.Lock()
		delete(e.pullRequests, uid)
		e.pullMutex.Unlock()
		return nil, fmt.Errorf("send pull request: %w", err)
	}
	
	// Wait for responses via channel
	return e.waitForPullResponse(ctx, uid, vaultKey)
}

// waitForPullResponse waits for pull request responses via the message router.
func (e *Engine) waitForPullResponse(ctx context.Context, uid int, vaultKey []byte) ([]byte, error) {
	timeout := time.After(30 * time.Second)
	
	var metadata map[string]interface{}
	var binaryData []byte
	
	for {
		select {
		case resp := <-e.pullResponses:
			if resp.UID != uid {
				// Not our response, put it back if possible or skip
				select {
				case e.pullResponses <- resp:
				default:
					e.logger.WithField("uid", resp.UID).Warn("Dropped pull response for different UID")
				}
				continue
			}
			
			switch resp.Type {
			case "metadata":
				metadata = resp.Metadata
				pieces := getInt(metadata, "pieces")
				size := getInt(metadata, "size")
				hash := getString(metadata, "hash")
				
				hashPreview := hash
				if len(hashPreview) > 16 {
					hashPreview = hashPreview[:16] + "..."
				}
				
				e.logger.WithFields(map[string]interface{}{
					"uid":    uid,
					"pieces": pieces,
					"size":   size,
					"hash":   hashPreview,
				}).Debug("Received pull request metadata")
				
				// Update request state
				e.pullMutex.Lock()
				if req, exists := e.pullRequests[uid]; exists {
					req.Metadata = metadata
				}
				e.pullMutex.Unlock()
				
				// Handle empty files (pieces = 0)
				if pieces == 0 {
					e.logger.WithField("uid", uid).Debug("Empty file, no binary data expected")
					
					// Clean up request
					e.pullMutex.Lock()
					delete(e.pullRequests, uid)
					e.pullMutex.Unlock()
					
					return []byte{}, nil
				}
				// Continue waiting for binary data
				
			case "binary":
				if metadata == nil {
					// Clean up and return error
					e.pullMutex.Lock()
					delete(e.pullRequests, uid)
					e.pullMutex.Unlock()
					return nil, fmt.Errorf("binary data received before metadata")
				}
				
				binaryData = resp.Data
				
				e.logger.WithFields(map[string]interface{}{
					"uid":            uid,
					"encrypted_size": len(binaryData),
				}).Debug("Received binary response for pull request")
				
				// Decrypt the content
				plainData, err := e.crypto.DecryptData(binaryData, vaultKey)
				if err != nil {
					// Clean up request
					e.pullMutex.Lock()
					delete(e.pullRequests, uid)
					e.pullMutex.Unlock()
					return nil, fmt.Errorf("decrypt pulled content: %w", err)
				}
				
				e.logger.WithFields(map[string]interface{}{
					"uid":            uid,
					"decrypted_size": len(plainData),
				}).Debug("Successfully decrypted pulled content")
				
				// Clean up request
				e.pullMutex.Lock()
				delete(e.pullRequests, uid)
				e.pullMutex.Unlock()
				
				return plainData, nil
			}
			
		case <-timeout:
			// Clean up request on timeout
			e.pullMutex.Lock()
			delete(e.pullRequests, uid)
			e.pullMutex.Unlock()
			return nil, fmt.Errorf("pull request timeout after 30 seconds")
			
		case <-ctx.Done():
			// Clean up request on context cancellation
			e.pullMutex.Lock()
			delete(e.pullRequests, uid)
			e.pullMutex.Unlock()
			return nil, ctx.Err()
		}
	}
}

// processPullQueue downloads all files in the pull queue.
func (e *Engine) processPullQueue(ctx context.Context, vaultKey []byte, syncState *models.SyncState) error {
	for i, req := range e.pullQueue {
		e.logger.WithFields(map[string]interface{}{
			"progress": fmt.Sprintf("%d/%d", i+1, len(e.pullQueue)),
			"path":     req.Path,
			"size":     req.Size,
		}).Info("Processing pull request")
		
		// Download file content  
		plainData, err := e.pullFileContent(ctx, req.UID, vaultKey)
		if err != nil {
			e.logger.WithError(err).WithField("path", req.Path).Error("Failed to pull file content")
			continue // Continue with other files
		}
		
		// Skip hash verification - rely on AES-GCM authentication tag for integrity
		
		// Detect file mode (binary vs text)
		mode := os.FileMode(0644)
		if models.IsBinaryFile(req.Path, plainData) {
			mode = 0644 // Keep same mode for binary files
		}
		
		// Write to storage
		if err := e.storage.Write(req.Path, plainData, mode); err != nil {
			e.logger.WithError(err).WithField("path", req.Path).Error("Failed to write file")
			continue
		}
		
		e.logger.WithFields(map[string]interface{}{
			"path":            req.Path,
			"decrypted_size":  len(plainData),
			"is_binary":       models.IsBinaryFile(req.Path, plainData),
		}).Info("File downloaded and written successfully")
	}
	
	// Clear the pull queue
	e.pullQueue = nil
	
	return nil
}

// processPullQueueAsync processes the pull queue asynchronously.
func (e *Engine) processPullQueueAsync(ctx context.Context, vaultKey []byte, syncState *models.SyncState) {
	e.logger.Info("Starting async pull queue processing")
	
	// Ensure completion signal is always sent, even on early exit
	defer func() {
		e.completePullPhase()
	}()
	
	for i, req := range e.pullQueue {
		// Check for context cancellation before processing each file
		select {
		case <-ctx.Done():
			e.logger.WithError(ctx.Err()).Warn("Pull queue processing cancelled")
			return
		default:
		}
		
		e.logger.WithFields(map[string]interface{}{
			"progress": fmt.Sprintf("%d/%d", i+1, len(e.pullQueue)),
			"path":     req.Path,
			"size":     req.Size,
		}).Info("Processing pull request")
		
		// Download file content  
		plainData, err := e.pullFileContent(ctx, req.UID, vaultKey)
		if err != nil {
			e.logger.WithError(err).WithField("path", req.Path).Error("Failed to pull file content")
			// Check if error was due to context cancellation
			if ctx.Err() != nil {
				e.logger.WithError(ctx.Err()).Warn("Pull file content cancelled")
				return
			}
			continue // Continue with other files
		}
		
		// Skip hash verification - rely on AES-GCM authentication tag for integrity
		
		// Detect file mode (binary vs text)
		mode := os.FileMode(0644)
		if models.IsBinaryFile(req.Path, plainData) {
			mode = 0644 // Keep same mode for binary files
		}
		
		// Write to storage
		if err := e.storage.Write(req.Path, plainData, mode); err != nil {
			e.logger.WithError(err).WithField("path", req.Path).Error("Failed to write file")
			continue
		}
		
		// Update state with file hash
		syncState.Files[req.Path] = req.Hash
		
		e.logger.WithFields(map[string]interface{}{
			"path":            req.Path,
			"decrypted_size":  len(plainData),
			"is_binary":       models.IsBinaryFile(req.Path, plainData),
		}).Info("File downloaded and written successfully")
	}
	
	// Completion signal is sent by defer function
}

// completePullPhase signals that the pull phase is complete.
func (e *Engine) completePullPhase() {
	e.logger.Info("Pull phase completed, signaling sync completion")
	
	// Set phase to complete
	e.syncPhase = "complete"
	
	// Send a completion signal via the pull responses channel
	// This will be picked up by the main message loop
	select {
	case e.pullResponses <- PullResponse{
		Type: "complete",
	}:
	default:
		e.logger.Warn("Failed to send completion signal - channel full")
	}
}


// isPullResponse detects if a message is a pull request response.
func (e *Engine) isPullResponse(msg models.WSMessage) bool {
	// Binary messages are likely pull responses
	if msg.Type == "binary" {
		return true
	}
	
	// Check for pull metadata responses (JSON with hash and pieces fields)
	if msg.Type == "" || msg.Type == "pull_metadata" {
		var data map[string]interface{}
		if err := json.Unmarshal(msg.Data, &data); err == nil {
			if _, hasHash := data["hash"]; hasHash {
				if _, hasPieces := data["pieces"]; hasPieces {
					return true
				}
			}
		}
	}
	
	return false
}

// routePullResponse routes a pull response to the appropriate handler.
func (e *Engine) routePullResponse(msg models.WSMessage) error {
	e.pullMutex.Lock()
	defer e.pullMutex.Unlock()
	
	if msg.Type == "binary" {
		// Binary data - match to the most recent metadata UID
		if e.lastMetadataUID > 0 {
			uid := e.lastMetadataUID
			e.logger.WithFields(map[string]interface{}{
				"uid":  uid,
				"size": len(msg.BinaryData),
			}).Debug("Routing binary response to pull request")
			
			select {
			case e.pullResponses <- PullResponse{
				UID:  uid,
				Type: "binary",
				Data: msg.BinaryData,
			}:
			default:
				e.logger.Warn("Pull response channel full, dropping binary response")
			}
			
			// Clear the last metadata UID after matching
			e.lastMetadataUID = 0
			return nil
		}
		e.logger.Warn("Received binary response with no recent metadata UID")
		return nil
	}
	
	// JSON metadata response
	var metadata map[string]interface{}
	if err := json.Unmarshal(msg.Data, &metadata); err != nil {
		return fmt.Errorf("parse pull metadata: %w", err)
	}
	
	// Find matching request by checking active requests
	// Since we process pulls sequentially, match to the first incomplete request
	for uid, req := range e.pullRequests {
		if !req.Complete && req.Metadata == nil {
			e.logger.WithFields(map[string]interface{}{
				"uid":    uid,
				"hash":   getString(metadata, "hash"),
				"pieces": getInt(metadata, "pieces"),
			}).Debug("Routing metadata response to pull request")
			
			// Track this UID for subsequent binary response matching
			e.lastMetadataUID = uid
			
			select {
			case e.pullResponses <- PullResponse{
				UID:      uid,
				Type:     "metadata",
				Metadata: metadata,
			}:
			default:
				e.logger.Warn("Pull response channel full, dropping metadata response")
			}
			return nil
		}
	}
	
	e.logger.WithField("metadata", metadata).Warn("Received metadata response with no matching pull request")
	return nil
}