package client

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/TheMichaelB/obsync/internal/config"
	"github.com/TheMichaelB/obsync/internal/crypto"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/models"
	"github.com/TheMichaelB/obsync/internal/services/auth"
	"github.com/TheMichaelB/obsync/internal/services/sync"
	"github.com/TheMichaelB/obsync/internal/services/vaults"
	"github.com/TheMichaelB/obsync/internal/state"
	"github.com/TheMichaelB/obsync/internal/storage"
	"github.com/TheMichaelB/obsync/internal/transport"
)

// Client provides the high-level API for Obsync operations.
type Client struct {
	Auth   *auth.Service
	Vaults *vaults.Service
	Sync   *sync.Service
	State  StateManager
	
	config    *config.Config
	logger    *events.Logger
	transport transport.Transport
	storage   storage.BlobStore
}

// StateManager provides state management operations.
type StateManager interface {
	ListStates() ([]*state.SyncStateInfo, error)
	LoadState(vaultID string) (*models.SyncState, error)
	Reset(vaultID string) error
}

// New creates a new Obsync client.
func New(cfg *config.Config, logger *events.Logger) (*Client, error) {
	// Create transport
	transportClient := transport.NewTransport(&cfg.API, logger)

	// Create crypto provider
	cryptoProvider := crypto.NewProvider()

	// Create state store
	stateStore, err := state.NewJSONStore(cfg.Storage.StateDir, logger)
	if err != nil {
		return nil, err
	}

	// Create blob store
	blobStore, err := storage.NewLocalStore(cfg.Storage.DataDir, logger)
	if err != nil {
		return nil, err
	}

	// Use configured token file path with tilde expansion
	tokenFile := cfg.Auth.TokenFile
	if tokenFile == "" {
		tokenFile = filepath.Join(cfg.Storage.StateDir, "auth", "token.json")
	}
	
	// Expand tilde in path
	if strings.HasPrefix(tokenFile, "~/") {
		homeDir, _ := os.UserHomeDir()
		tokenFile = filepath.Join(homeDir, tokenFile[2:])
	}
	
	// Ensure token directory exists
	tokenDir := filepath.Dir(tokenFile)
	if err := os.MkdirAll(tokenDir, 0700); err != nil {
		logger.WithError(err).Warn("Failed to create token directory")
	}

	// Create services
	authService := auth.NewService(transportClient, tokenFile, logger)
	vaultsService := vaults.NewService(transportClient, cryptoProvider, logger)
	
	syncConfig := &sync.SyncConfig{
		MaxConcurrent: cfg.Sync.MaxConcurrent,
		ChunkSize:     cfg.Sync.ChunkSize,
	}
	syncService := sync.NewService(
		transportClient,
		cryptoProvider,
		stateStore,
		blobStore,
		authService,
		vaultsService,
		syncConfig,
		logger,
	)

	client := &Client{
		Auth:      authService,
		Vaults:    vaultsService,
		Sync:      syncService,
		State:     &stateManager{store: stateStore},
		config:    cfg,
		logger:    logger,
		transport: transportClient,
		storage:   blobStore,
	}

	return client, nil
}

// SetStorageBase sets the base directory for file storage.
func (c *Client) SetStorageBase(basePath string) error {
	if localStore, ok := c.storage.(*storage.LocalStore); ok {
		return localStore.SetBasePath(basePath)
	}
	return nil
}

// stateManager implements StateManager interface.
type stateManager struct {
	store state.Store
}

func (sm *stateManager) ListStates() ([]*state.SyncStateInfo, error) {
	vaultIDs, err := sm.store.List()
	if err != nil {
		return nil, err
	}

	var states []*state.SyncStateInfo
	for _, vaultID := range vaultIDs {
		syncState, err := sm.store.Load(vaultID)
		if err != nil {
			continue // Skip states that can't be loaded
		}

		stateInfo := &state.SyncStateInfo{
			VaultID:      syncState.VaultID,
			Version:      syncState.Version,
			Files:        syncState.Files,
			LastSyncTime: syncState.LastSyncTime,
			LastError:    syncState.LastError,
		}
		states = append(states, stateInfo)
	}

	return states, nil
}

func (sm *stateManager) LoadState(vaultID string) (*models.SyncState, error) {
	return sm.store.Load(vaultID)
}

func (sm *stateManager) Reset(vaultID string) error {
	return sm.store.Reset(vaultID)
}