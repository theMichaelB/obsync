package client

import (
	"fmt"
	"path/filepath"
	
	"github.com/TheMichaelB/obsync/internal/config"
	"github.com/TheMichaelB/obsync/internal/crypto"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/lambda/adapters"
	"github.com/TheMichaelB/obsync/internal/services/auth"
	"github.com/TheMichaelB/obsync/internal/services/sync"
	"github.com/TheMichaelB/obsync/internal/services/vaults"
	"github.com/TheMichaelB/obsync/internal/transport"
)

// NewLambdaClient creates a client optimized for Lambda
func NewLambdaClient(cfg *config.Config, logger *events.Logger) (*Client, error) {
	// Detect if running in Lambda
	if !config.IsLambdaEnvironment() {
		return New(cfg, logger)
	}
	
	logger.Info("Initializing Lambda-optimized client")
	
	// Create Lambda-specific components
	lambdaCfg := config.LoadLambdaConfig()
	
	// Create transport
	transportClient := transport.NewTransport(&cfg.API, logger)
	
	// Create S3 storage
	if lambdaCfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET environment variable required for Lambda")
	}
	
	s3Store, err := adapters.NewS3Store(
		lambdaCfg.S3Bucket, 
		lambdaCfg.S3Prefix, 
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("create s3 store: %w", err)
	}
	
	// Create DynamoDB state
	dynamoStore, err := adapters.NewDynamoDBStore(
		lambdaCfg.StateTableName,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("create dynamodb store: %w", err)
	}
	
	// Create crypto provider
	cryptoProvider := crypto.NewProvider()
	
	// Create services
	tokenFile := filepath.Join("/tmp/obsync/auth", "token.json")
	authService := auth.NewService(transportClient, tokenFile, logger)
	vaultService := vaults.NewService(transportClient, cryptoProvider, logger)
	
	// Create sync config
	syncConfig := &sync.SyncConfig{
		ChunkSize:     cfg.Sync.ChunkSize,
		MaxConcurrent: cfg.Sync.MaxConcurrent,
	}
	
	// Create sync service
	syncService := sync.NewService(
		transportClient,
		cryptoProvider,
		dynamoStore,
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
		State:     &stateManager{store: dynamoStore},
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