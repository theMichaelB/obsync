package sync

import (
	"context"
	"fmt"

	"github.com/yourusername/obsync/internal/crypto"
	"github.com/yourusername/obsync/internal/events"
	"github.com/yourusername/obsync/internal/services/auth"
	"github.com/yourusername/obsync/internal/services/vaults"
	"github.com/yourusername/obsync/internal/state"
	"github.com/yourusername/obsync/internal/storage"
	"github.com/yourusername/obsync/internal/transport"
)

// Service provides high-level sync operations.
type Service struct {
	auth   *auth.Service
	vaults *vaults.Service
	engine *Engine
	logger *events.Logger

	// User credentials (for key derivation)
	email    string
	password string
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

	// Get vault information (includes host)
	vault, err := s.vaults.GetVault(ctx, vaultID)
	if err != nil {
		return fmt.Errorf("get vault info: %w", err)
	}

	vaultKey, err := s.vaults.GetVaultKey(ctx, vaultID, s.email, s.password)
	if err != nil {
		return fmt.Errorf("get vault key: %w", err)
	}

	// Start sync
	return s.engine.Sync(ctx, vaultID, vault.Host, vaultKey, vault.EncryptionInfo.EncryptionVersion, opts.Initial)
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