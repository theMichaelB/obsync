package client

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
	
	"github.com/TheMichaelB/obsync/internal/config"
	"github.com/TheMichaelB/obsync/internal/crypto"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/lambda/adapters"
	"github.com/TheMichaelB/obsync/internal/services/auth"
	"github.com/TheMichaelB/obsync/internal/services/sync"
	"github.com/TheMichaelB/obsync/internal/services/vaults"
	"github.com/TheMichaelB/obsync/internal/transport"
)

// NewLambdaClient creates a client optimized for Lambda or S3 storage
func NewLambdaClient(cfg *config.Config, logger *events.Logger) (*Client, error) {
	logger.Info("Initializing S3 storage client")
	
	// Create Lambda-specific components
	lambdaCfg := config.LoadLambdaConfig()
	
	// Create transport
	transportClient := transport.NewTransport(&cfg.API, logger)
	
	// Create S3 storage
	if lambdaCfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET environment variable required for S3 storage mode")
	}
	
	s3Store, err := adapters.NewS3Store(
		lambdaCfg.S3Bucket, 
		lambdaCfg.S3Prefix, 
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("create s3 store: %w", err)
	}
	
	// Create S3 state store
	s3StateStore, err := adapters.NewS3StateStore(
		lambdaCfg.S3Bucket,
		lambdaCfg.S3StatePrefix,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("create s3 state store: %w", err)
	}
	
	// Create crypto provider
	cryptoProvider := crypto.NewProvider()
	
	// Create services - use different token path depending on environment
	var tokenFile string
	if config.IsLambdaEnvironment() {
		tokenFile = filepath.Join("/tmp/obsync/auth", "token.json")
	} else {
		// For CLI S3 mode, use the configured token file path
		tokenFile = cfg.Auth.TokenFile
		if tokenFile == "" {
			tokenFile = filepath.Join(cfg.Storage.StateDir, "auth", "token.json")
		}
	}
	authService := auth.NewService(transportClient, tokenFile, logger)
	vaultService := vaults.NewService(transportClient, cryptoProvider, logger)
	
	// Create sync config
	syncConfig := &sync.SyncConfig{
		ChunkSize:     cfg.Sync.ChunkSize,
		MaxConcurrent: cfg.Sync.MaxConcurrent,
	}
	
	// Download states on startup if enabled (mainly for Lambda optimization)
	if lambdaCfg.DownloadOnStartup && config.IsLambdaEnvironment() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		
		if err := s3StateStore.DownloadStatesOnStartup(ctx); err != nil {
			logger.WithError(err).Warn("Failed to download states on startup, continuing without cache")
		}
	}
	
	// Create sync service
	syncService := sync.NewService(
		transportClient,
		cryptoProvider,
		s3StateStore,
		s3Store,
		authService,
		vaultService,
		syncConfig,
		logger,
	)
	
	// Create client with Lambda components
	client := &Client{
		Auth:      authService,
		Vaults:    vaultService,
		Sync:      syncService,
		State:     &stateManager{store: s3StateStore},
		config:    cfg,
		logger:    logger,
		transport: transportClient,
		storage:   s3Store,
	}
	
	return client, nil
}

// IsRunningInLambda checks if the client is running in Lambda environment
func IsRunningInLambda() bool {
	return config.IsLambdaEnvironment()
}

// GetLambdaConfig returns Lambda-specific configuration if running in Lambda
func GetLambdaConfig() *config.LambdaConfig {
	if config.IsLambdaEnvironment() {
		return config.LoadLambdaConfig()
	}
	return nil
}