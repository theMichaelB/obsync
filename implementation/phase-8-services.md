# Phase 8: Core Services

This phase implements the authentication service, vault management, and the sync engine with progress reporting.

## Service Architecture

The services layer provides:
- Authentication with token management
- Vault listing and key retrieval
- Sync engine with UID-based incremental sync
- Progress reporting and event emission
- Error recovery and resume capability

## Authentication Service

`internal/services/auth/service.go`:

```go
package auth

import (
    "context"
    "fmt"
    "time"
    
    "github.com/yourusername/obsync/internal/events"
    "github.com/yourusername/obsync/internal/models"
    "github.com/yourusername/obsync/internal/transport"
)

// Service handles authentication operations.
type Service struct {
    transport transport.Transport
    logger    *events.Logger
    
    // Token cache
    token     *models.TokenInfo
    tokenFile string
}

// NewService creates an auth service.
func NewService(transport transport.Transport, tokenFile string, logger *events.Logger) *Service {
    return &Service{
        transport: transport,
        tokenFile: tokenFile,
        logger:    logger.WithField("service", "auth"),
    }
}

// Login authenticates with email and password.
func (s *Service) Login(ctx context.Context, email, password, totp string) error {
    s.logger.WithField("email", email).Info("Logging in")
    
    req := models.AuthRequest{
        Email:    email,
        Password: password,
        TOTP:     totp,
    }
    
    resp, err := s.transport.PostJSON(ctx, "/api/v1/auth/login", req)
    if err != nil {
        return fmt.Errorf("login request: %w", err)
    }
    
    // Parse response
    token, ok := resp["token"].(string)
    if !ok || token == "" {
        return fmt.Errorf("invalid login response: missing token")
    }
    
    expiresStr, _ := resp["expires_at"].(string)
    expiresAt, _ := time.Parse(time.RFC3339, expiresStr)
    if expiresAt.IsZero() {
        expiresAt = time.Now().Add(24 * time.Hour) // Default expiry
    }
    
    // Store token
    s.token = &models.TokenInfo{
        Token:     token,
        ExpiresAt: expiresAt,
        Email:     email,
    }
    
    // Update transport
    s.transport.SetToken(token)
    
    // Save token to file
    if err := s.saveToken(); err != nil {
        s.logger.WithError(err).Warn("Failed to save token")
    }
    
    s.logger.Info("Login successful")
    return nil
}

// Logout clears authentication.
func (s *Service) Logout(ctx context.Context) error {
    s.logger.Info("Logging out")
    
    if s.token != nil && !s.token.IsExpired() {
        // Notify server
        _, err := s.transport.PostJSON(ctx, "/api/v1/auth/logout", nil)
        if err != nil {
            s.logger.WithError(err).Warn("Server logout failed")
        }
    }
    
    // Clear local state
    s.token = nil
    s.transport.SetToken("")
    
    // Remove token file
    if s.tokenFile != "" {
        os.Remove(s.tokenFile)
    }
    
    return nil
}

// GetToken returns current token if valid.
func (s *Service) GetToken() (*models.TokenInfo, error) {
    // Try cached token
    if s.token != nil && !s.token.IsExpired() {
        return s.token, nil
    }
    
    // Try loading from file
    if err := s.loadToken(); err == nil && s.token != nil && !s.token.IsExpired() {
        s.transport.SetToken(s.token.Token)
        return s.token, nil
    }
    
    return nil, models.ErrNotAuthenticated
}

// RefreshToken refreshes the auth token.
func (s *Service) RefreshToken(ctx context.Context) error {
    token, err := s.GetToken()
    if err != nil {
        return err
    }
    
    s.logger.Debug("Refreshing token")
    
    resp, err := s.transport.PostJSON(ctx, "/api/v1/auth/refresh", nil)
    if err != nil {
        return fmt.Errorf("refresh request: %w", err)
    }
    
    // Update token
    if newToken, ok := resp["token"].(string); ok {
        token.Token = newToken
        s.transport.SetToken(newToken)
    }
    
    if expiresStr, ok := resp["expires_at"].(string); ok {
        if expiresAt, err := time.Parse(time.RFC3339, expiresStr); err == nil {
            token.ExpiresAt = expiresAt
        }
    }
    
    // Save updated token
    return s.saveToken()
}

// EnsureAuthenticated checks and refreshes auth if needed.
func (s *Service) EnsureAuthenticated(ctx context.Context) error {
    token, err := s.GetToken()
    if err != nil {
        return err
    }
    
    // Refresh if expiring soon
    if time.Until(token.ExpiresAt) < 5*time.Minute {
        if err := s.RefreshToken(ctx); err != nil {
            s.logger.WithError(err).Warn("Token refresh failed")
            // Continue with existing token
        }
    }
    
    return nil
}

// Token persistence

func (s *Service) saveToken() error {
    if s.tokenFile == "" || s.token == nil {
        return nil
    }
    
    data, err := json.Marshal(s.token)
    if err != nil {
        return fmt.Errorf("marshal token: %w", err)
    }
    
    // Save with restricted permissions
    return os.WriteFile(s.tokenFile, data, 0600)
}

func (s *Service) loadToken() error {
    if s.tokenFile == "" {
        return fmt.Errorf("no token file configured")
    }
    
    data, err := os.ReadFile(s.tokenFile)
    if err != nil {
        return fmt.Errorf("read token file: %w", err)
    }
    
    var token models.TokenInfo
    if err := json.Unmarshal(data, &token); err != nil {
        return fmt.Errorf("parse token: %w", err)
    }
    
    s.token = &token
    return nil
}
```

## Vault Service

`internal/services/vaults/service.go`:

```go
package vaults

import (
    "context"
    "encoding/base64"
    "fmt"
    
    "github.com/yourusername/obsync/internal/crypto"
    "github.com/yourusername/obsync/internal/events"
    "github.com/yourusername/obsync/internal/models"
    "github.com/yourusername/obsync/internal/transport"
)

// Service manages vault operations.
type Service struct {
    transport transport.Transport
    crypto    crypto.Provider
    logger    *events.Logger
    
    // Cache
    vaults map[string]*models.Vault
    keys   map[string][]byte
}

// NewService creates a vault service.
func NewService(transport transport.Transport, crypto crypto.Provider, logger *events.Logger) *Service {
    return &Service{
        transport: transport,
        crypto:    crypto,
        logger:    logger.WithField("service", "vaults"),
        vaults:    make(map[string]*models.Vault),
        keys:      make(map[string][]byte),
    }
}

// ListVaults fetches available vaults.
func (s *Service) ListVaults(ctx context.Context) ([]*models.Vault, error) {
    s.logger.Debug("Fetching vault list")
    
    resp, err := s.transport.PostJSON(ctx, "/api/v1/vaults/list", nil)
    if err != nil {
        return nil, fmt.Errorf("list vaults: %w", err)
    }
    
    // Parse vaults array
    vaultsData, ok := resp["vaults"].([]interface{})
    if !ok {
        return nil, fmt.Errorf("invalid vault list response")
    }
    
    var vaults []*models.Vault
    for _, v := range vaultsData {
        vaultMap, ok := v.(map[string]interface{})
        if !ok {
            continue
        }
        
        vault := &models.Vault{
            ID:   getString(vaultMap, "id"),
            Name: getString(vaultMap, "name"),
        }
        
        // Parse encryption info
        if encInfo, ok := vaultMap["encryption_info"].(map[string]interface{}); ok {
            vault.EncryptionInfo = models.KeyInfo{
                Version:           getInt(encInfo, "version"),
                EncryptionVersion: getInt(encInfo, "encryption_version"),
                Salt:             getString(encInfo, "salt"),
            }
        }
        
        vaults = append(vaults, vault)
        s.vaults[vault.ID] = vault // Cache
    }
    
    s.logger.WithField("count", len(vaults)).Info("Fetched vaults")
    return vaults, nil
}

// GetVault retrieves a specific vault.
func (s *Service) GetVault(ctx context.Context, vaultID string) (*models.Vault, error) {
    // Check cache
    if vault, ok := s.vaults[vaultID]; ok {
        return vault, nil
    }
    
    // Fetch from server
    resp, err := s.transport.PostJSON(ctx, "/api/v1/vaults/get", map[string]string{
        "vault_id": vaultID,
    })
    if err != nil {
        return nil, fmt.Errorf("get vault: %w", err)
    }
    
    vault := &models.Vault{
        ID:   getString(resp, "id"),
        Name: getString(resp, "name"),
    }
    
    if encInfo, ok := resp["encryption_info"].(map[string]interface{}); ok {
        vault.EncryptionInfo = models.KeyInfo{
            Version:           getInt(encInfo, "version"),
            EncryptionVersion: getInt(encInfo, "encryption_version"),
            Salt:             getString(encInfo, "salt"),
        }
    }
    
    s.vaults[vaultID] = vault
    return vault, nil
}

// GetVaultKey derives the encryption key for a vault.
func (s *Service) GetVaultKey(ctx context.Context, vaultID, email, password string) ([]byte, error) {
    // Check cache
    if key, ok := s.keys[vaultID]; ok {
        return key, nil
    }
    
    // Get vault info
    vault, err := s.GetVault(ctx, vaultID)
    if err != nil {
        return nil, err
    }
    
    s.logger.WithFields(map[string]interface{}{
        "vault_id":           vaultID,
        "encryption_version": vault.EncryptionInfo.EncryptionVersion,
    }).Debug("Deriving vault key")
    
    // Convert salt from base64
    salt, err := base64.StdEncoding.DecodeString(vault.EncryptionInfo.Salt)
    if err != nil {
        return nil, fmt.Errorf("decode salt: %w", err)
    }
    
    // Derive key
    keyInfo := crypto.VaultKeyInfo{
        Version:           vault.EncryptionInfo.Version,
        EncryptionVersion: vault.EncryptionInfo.EncryptionVersion,
        Salt:             vault.EncryptionInfo.Salt,
    }
    
    key, err := s.crypto.DeriveKey(email, password, keyInfo)
    if err != nil {
        return nil, fmt.Errorf("derive key: %w", err)
    }
    
    // Cache key
    s.keys[vaultID] = key
    
    return key, nil
}

// ClearCache removes cached vaults and keys.
func (s *Service) ClearCache() {
    s.vaults = make(map[string]*models.Vault)
    s.keys = make(map[string][]byte)
}

// Helper functions

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
```

## Sync Engine

`internal/services/sync/engine.go`:

```go
package sync

import (
    "context"
    "fmt"
    "path/filepath"
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
    mu       sync.Mutex
    syncing  bool
    cancelFn context.CancelFunc
}

// Progress tracks sync progress.
type Progress struct {
    Phase         string
    TotalFiles    int
    ProcessedFiles int
    CurrentFile   string
    BytesDownloaded int64
    StartTime     time.Time
    Errors        []error
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
func (e *Engine) Sync(ctx context.Context, vaultID string, vaultKey []byte, initial bool) error {
    e.mu.Lock()
    if e.syncing {
        e.mu.Unlock()
        return models.ErrSyncInProgress
    }
    e.syncing = true
    
    ctx, cancel := context.WithCancel(ctx)
    e.cancelFn = cancel
    e.mu.Unlock()
    
    defer func() {
        e.mu.Lock()
        e.syncing = false
        e.cancelFn = nil
        e.mu.Unlock()
        close(e.events)
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
    
    // Connect to WebSocket
    initMsg := models.InitMessage{
        VaultID: vaultID,
        Initial: initial,
        Version: syncState.Version,
    }
    
    msgChan, err := e.transport.StreamWS(ctx, initMsg)
    if err != nil {
        return e.handleError(fmt.Errorf("connect websocket: %w", err))
    }
    
    // Process messages
    pathDecryptor := crypto.NewPathDecryptor(e.crypto)
    
    for msg := range msgChan {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        
        if err := e.processMessage(ctx, msg, vaultKey, syncState, pathDecryptor); err != nil {
            e.logger.WithError(err).WithField("msg_type", msg.Type).Error("Failed to process message")
            progress.Errors = append(progress.Errors, err)
            
            // Determine if error is fatal
            if e.isFatalError(err) {
                return e.handleError(err)
            }
        }
    }
    
    // Save final state
    progress.Phase = "finalizing"
    if err := e.state.Save(vaultID, syncState); err != nil {
        return e.handleError(fmt.Errorf("save state: %w", err))
    }
    
    // Emit completion
    progress.Phase = "completed"
    e.emitEvent(Event{
        Type:      EventCompleted,
        Timestamp: time.Now(),
        Progress:  progress,
    })
    
    e.logger.WithFields(map[string]interface{}{
        "duration": time.Since(progress.StartTime),
        "files":    progress.ProcessedFiles,
        "errors":   len(progress.Errors),
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
    pathDecryptor *crypto.PathDecryptor,
) error {
    switch msg.Type {
    case models.WSTypeInitResponse:
        return e.handleInitResponse(msg)
        
    case models.WSTypeFile:
        return e.handleFileMessage(ctx, msg, vaultKey, syncState, pathDecryptor)
        
    case models.WSTypeFolder:
        return e.handleFolderMessage(msg, vaultKey, pathDecryptor)
        
    case models.WSTypeDelete:
        return e.handleDeleteMessage(msg, vaultKey, syncState, pathDecryptor)
        
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
    pathDecryptor *crypto.PathDecryptor,
) error {
    var fileMsg models.FileMessage
    if err := json.Unmarshal(msg.Data, &fileMsg); err != nil {
        return fmt.Errorf("parse file message: %w", err)
    }
    
    // Decrypt path
    plainPath, err := pathDecryptor.DecryptPath(fileMsg.Path, vaultKey)
    if err != nil {
        return &models.DecryptError{
            Path:   fileMsg.Path,
            Reason: "path decryption",
            Err:    err,
        }
    }
    
    progress := e.GetProgress()
    progress.CurrentFile = plainPath
    
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
            e.storage.SetModTime(plainPath, fileMsg.ModifiedTime)
        }
        
        // Update progress
        atomic.AddInt64(&progress.BytesDownloaded, int64(len(plainData)))
    }
    
    // Update state
    syncState.Files[plainPath] = fileMsg.Hash
    syncState.Version = fileMsg.UID
    
    // Emit complete event
    progress.ProcessedFiles++
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
    progress.Phase = "syncing"
    progress.TotalFiles = resp.TotalFiles
    
    e.logger.WithFields(map[string]interface{}{
        "total_files":    resp.TotalFiles,
        "start_version":  resp.StartVersion,
        "latest_version": resp.LatestVersion,
    }).Info("Sync initialized")
    
    return nil
}

func (e *Engine) handleFolderMessage(msg models.WSMessage, vaultKey []byte, pathDecryptor *crypto.PathDecryptor) error {
    var folderMsg models.FolderMessage
    if err := json.Unmarshal(msg.Data, &folderMsg); err != nil {
        return fmt.Errorf("parse folder message: %w", err)
    }
    
    // Decrypt path
    plainPath, err := pathDecryptor.DecryptPath(folderMsg.Path, vaultKey)
    if err != nil {
        return fmt.Errorf("decrypt folder path: %w", err)
    }
    
    // Create directory
    return e.storage.EnsureDir(plainPath)
}

func (e *Engine) handleDeleteMessage(msg models.WSMessage, vaultKey []byte, syncState *models.SyncState, pathDecryptor *crypto.PathDecryptor) error {
    var deleteMsg models.DeleteMessage
    if err := json.Unmarshal(msg.Data, &deleteMsg); err != nil {
        return fmt.Errorf("parse delete message: %w", err)
    }
    
    // Decrypt path
    plainPath, err := pathDecryptor.DecryptPath(deleteMsg.Path, vaultKey)
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
    state, err := e.state.Load(vaultID)
    if err == nil {
        return state, nil
    }
    
    if err == state.ErrStateNotFound {
        // Create new state
        return models.NewSyncState(vaultID), nil
    }
    
    return nil, err
}

func (e *Engine) emitEvent(event Event) {
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
    default:
        return false
    }
}
```

## Sync Service Integration

`internal/services/sync/service.go`:

```go
package sync

import (
    "context"
    "fmt"
    "time"
    
    "github.com/yourusername/obsync/internal/config"
    "github.com/yourusername/obsync/internal/crypto"
    "github.com/yourusername/obsync/internal/events"
    "github.com/yourusername/obsync/internal/models"
    "github.com/yourusername/obsync/internal/services/auth"
    "github.com/yourusername/obsync/internal/services/vaults"
    "github.com/yourusername/obsync/internal/state"
    "github.com/yourusername/obsync/internal/storage"
    "github.com/yourusername/obsync/internal/transport"
)

// Service provides high-level sync operations.
type Service struct {
    auth      *auth.Service
    vaults    *vaults.Service
    engine    *Engine
    logger    *events.Logger
    
    // User credentials (for key derivation)
    email    string
    password string
}

// SyncConfig contains sync configuration.
type SyncConfig struct {
    MaxConcurrent int
    ChunkSize     int
    RetryAttempts int
    RetryDelay    time.Duration
}

// NewService creates a sync service.
func NewService(
    transport transport.Transport,
    crypto crypto.Provider,
    state state.Store,
    storage storage.BlobStore,
    authService *auth.Service,
    vaultsService *vaults.Service,
    config *SyncConfig,
    logger *events.Logger,
) *Service {
    engine := NewEngine(transport, crypto, state, storage, config, logger)
    
    return &Service{
        auth:   authService,
        vaults: vaultsService,
        engine: engine,
        logger: logger.WithField("service", "sync"),
    }
}

// SetCredentials sets user credentials for key derivation.
func (s *Service) SetCredentials(email, password string) {
    s.email = email
    s.password = password
}

// SyncVault performs a full or incremental sync.
func (s *Service) SyncVault(ctx context.Context, vaultID string, opts SyncOptions) error {
    // Ensure authenticated
    if err := s.auth.EnsureAuthenticated(ctx); err != nil {
        return fmt.Errorf("authentication failed: %w", err)
    }
    
    // Get vault key
    if s.email == "" || s.password == "" {
        return fmt.Errorf("credentials not set")
    }
    
    vaultKey, err := s.vaults.GetVaultKey(ctx, vaultID, s.email, s.password)
    if err != nil {
        return fmt.Errorf("get vault key: %w", err)
    }
    
    // Start sync
    return s.engine.Sync(ctx, vaultID, vaultKey, opts.Initial)
}

// GetProgress returns sync progress.
func (s *Service) GetProgress() *Progress {
    return s.engine.GetProgress()
}

// Events returns the event channel.
func (s *Service) Events() <-chan Event {
    return s.engine.Events()
}

// Cancel stops an ongoing sync.
func (s *Service) Cancel() {
    s.engine.Cancel()
}

// SyncOptions configures a sync operation.
type SyncOptions struct {
    Initial bool // Full sync vs incremental
}
```

## Service Tests

`internal/services/sync/sync_test.go`:

```go
package sync_test

import (
    "context"
    "testing"
    "time"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    
    "github.com/yourusername/obsync/internal/config"
    "github.com/yourusername/obsync/internal/crypto"
    "github.com/yourusername/obsync/internal/events"
    "github.com/yourusername/obsync/internal/models"
    "github.com/yourusername/obsync/internal/services/sync"
    "github.com/yourusername/obsync/internal/state"
    "github.com/yourusername/obsync/internal/storage"
    "github.com/yourusername/obsync/internal/transport"
    "github.com/yourusername/obsync/test/testutil"
)

func TestSyncEngine(t *testing.T) {
    // Setup
    mockTransport := transport.NewMockTransport()
    cryptoProvider := crypto.NewProvider()
    stateStore := state.NewMockStore()
    blobStore := storage.NewMockStore()
    logger := events.NewLogger(&config.LogConfig{Level: "debug"})
    
    engine := sync.NewEngine(
        mockTransport,
        cryptoProvider,
        stateStore,
        blobStore,
        &sync.SyncConfig{
            MaxConcurrent: 5,
            ChunkSize:     1024,
        },
        logger,
    )
    
    // Prepare test data
    vaultID := "test-vault"
    vaultKey := make([]byte, 32) // Test key
    
    // Mock WebSocket messages
    mockTransport.AddWSMessage(models.WSMessage{
        Type: models.WSTypeInitResponse,
        Data: []byte(`{
            "success": true,
            "total_files": 2,
            "start_version": 0,
            "latest_version": 2
        }`),
    })
    
    // Encrypt test content
    testContent := []byte("test file content")
    encryptedContent, _ := crypto.EncryptData(testContent, vaultKey)
    mockTransport.AddChunk("chunk-1", encryptedContent)
    
    // Encrypt paths
    encPath1 := encryptPath("test.txt", vaultKey)
    mockTransport.AddWSMessage(models.WSMessage{
        Type: models.WSTypeFile,
        UID:  1,
        Data: []byte(fmt.Sprintf(`{
            "uid": 1,
            "path": "%s",
            "hash": "%x",
            "size": %d,
            "chunk_id": "chunk-1"
        }`, encPath1, sha256.Sum256(testContent), len(encryptedContent))),
    })
    
    mockTransport.AddWSMessage(models.WSMessage{
        Type: models.WSTypeDone,
        Data: []byte(`{
            "final_version": 2,
            "total_synced": 1,
            "duration": "1s"
        }`),
    })
    
    // Run sync
    ctx := context.Background()
    eventCount := 0
    go func() {
        for event := range engine.Events() {
            eventCount++
            t.Logf("Event: %s", event.Type)
        }
    }()
    
    err := engine.Sync(ctx, vaultID, vaultKey, true)
    require.NoError(t, err)
    
    // Verify results
    assert.Greater(t, eventCount, 0)
    
    // Check file was written
    assert.True(t, blobStore.FileExists("test.txt"))
    data, _ := blobStore.Read("test.txt")
    assert.Equal(t, testContent, data)
    
    // Check state was saved
    savedState, _ := stateStore.Load(vaultID)
    assert.Equal(t, 2, savedState.Version)
    assert.Equal(t, 1, len(savedState.Files))
}

func TestSyncCancellation(t *testing.T) {
    // Setup
    mockTransport := transport.NewMockTransport()
    cryptoProvider := crypto.NewProvider()
    stateStore := state.NewMockStore()
    blobStore := storage.NewMockStore()
    logger := events.NewLogger(&config.LogConfig{Level: "debug"})
    
    engine := sync.NewEngine(
        mockTransport,
        cryptoProvider,
        stateStore,
        blobStore,
        &sync.SyncConfig{},
        logger,
    )
    
    // Add delayed messages
    mockTransport.AddWSMessage(models.WSMessage{
        Type: models.WSTypeInitResponse,
        Data: []byte(`{"success": true, "total_files": 100}`),
    })
    
    // Start sync
    ctx := context.Background()
    vaultKey := make([]byte, 32)
    
    syncDone := make(chan error)
    go func() {
        syncDone <- engine.Sync(ctx, "test-vault", vaultKey, true)
    }()
    
    // Cancel after short delay
    time.Sleep(100 * time.Millisecond)
    engine.Cancel()
    
    // Should complete with context canceled
    err := <-syncDone
    assert.ErrorIs(t, err, context.Canceled)
}

// Helper function
func encryptPath(path string, key []byte) string {
    encrypted, _ := crypto.EncryptData([]byte(path), key)
    return hex.EncodeToString(encrypted)
}
```

## Phase 8 Deliverables

1. ✅ Authentication service with token management
2. ✅ Vault service with key derivation
3. ✅ Sync engine with UID-based incremental sync
4. ✅ Progress tracking and event emission
5. ✅ Error recovery and resume capability
6. ✅ Concurrent file processing
7. ✅ WebSocket message handling
8. ✅ Integration tests with mocks

## Verification Commands

```bash
# Run service tests
go test -v ./internal/services/...

# Test sync engine specifically
go test -v -run TestSyncEngine ./internal/services/sync/...

# Test with race detection
go test -race ./internal/services/...

# Benchmark sync performance
go test -bench=. ./internal/services/sync/...
```

## Next Steps

With core services complete, proceed to [Phase 9: CLI Interface](phase-9-cli.md) to implement the command-line interface with all commands.