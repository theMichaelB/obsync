package sync

import (
	"context"
	"fmt"

	"github.com/TheMichaelB/obsync/internal/creds"
	"github.com/TheMichaelB/obsync/internal/crypto"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/services/auth"
	"github.com/TheMichaelB/obsync/internal/services/vaults"
	"github.com/TheMichaelB/obsync/internal/state"
	"github.com/TheMichaelB/obsync/internal/storage"
	"github.com/TheMichaelB/obsync/internal/transport"
)

// Service provides high-level sync operations.
type Service struct {
	auth   *auth.Service
	vaults *vaults.Service
	engine *Engine
	logger *events.Logger

	// Combined credentials
	creds    *creds.Combined
	// Legacy: User credentials (for key derivation) - to be deprecated
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

// SetCombinedCredentials sets combined credentials.
func (s *Service) SetCombinedCredentials(c *creds.Combined) {
	s.creds = c
	// Also set legacy fields for compatibility
	if c != nil {
		s.email = c.Auth.Email
		s.password = c.Auth.Password
	}
}

// SetCredentials sets user credentials for key derivation (legacy).
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

	// Get credentials from combined or legacy
	email := s.email
	password := s.password
	if s.creds != nil {
		email = s.creds.Auth.Email
		password = s.creds.Auth.Password
	}
	
	if email == "" || password == "" {
		return fmt.Errorf("credentials not set")
	}

	// Get vault information (includes host)
	vault, err := s.vaults.GetVault(ctx, vaultID)
	if err != nil {
		return fmt.Errorf("get vault info: %w", err)
	}

	vaultKey, err := s.vaults.GetVaultKey(ctx, vaultID, email, password)
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