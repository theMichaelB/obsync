package handler

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "strconv"
    "time"
    
    "github.com/TheMichaelB/obsync/internal/client"
    "github.com/TheMichaelB/obsync/internal/config"
    "github.com/TheMichaelB/obsync/internal/creds"
    "github.com/TheMichaelB/obsync/internal/events"
    syncsvc "github.com/TheMichaelB/obsync/internal/services/sync"
)

// Event represents the Lambda input event
type Event struct {
	// Core sync parameters
	Action   string `json:"action"`              // "sync"
	VaultID  string `json:"vault_id,omitempty"`  // Empty means all vaults
	SyncType string `json:"sync_type"`           // "complete" or "incremental"
	
	// S3 destination override (optional)
	DestBucket string `json:"dest_bucket,omitempty"`
	DestPrefix string `json:"dest_prefix,omitempty"`
}

// Response represents the Lambda response
type Response struct {
	Success      bool              `json:"success"`
	Message      string            `json:"message"`
	VaultsSynced []string          `json:"vaults_synced"`
	FilesCount   int               `json:"files_count"`
	Errors       []string          `json:"errors,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Handler struct {
    client *client.Client
    logger *events.Logger
    creds  *creds.Combined  // Combined credentials
}

func NewHandler() (*Handler, error) {
	// Load configuration from environment
	cfg, err := loadLambdaConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	
	// Create logger that writes to CloudWatch
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	logCfg := &config.LogConfig{
		Level:  logLevel,
		Format: "json",
	}
	logger, err := events.NewLogger(logCfg)
	if err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}
	
    // Create client with Lambda adapters
    lambdaClient, err := client.NewLambdaClient(cfg, logger)
    if err != nil {
        return nil, fmt.Errorf("create client: %w", err)
    }

    h := &Handler{
        client: lambdaClient,
        logger: logger,
    }

    // Load combined credentials from secret if configured
    if v := os.Getenv("OBSYNC_SECRET_NAME"); v != "" {
        logger.WithField("secret_name", v).Info("Loading credentials from Secrets Manager")
        combinedCreds, err := creds.LoadFromSecret(context.Background(), v)
        if err != nil {
            logger.WithError(err).Error("Failed to load combined secret")
            return nil, fmt.Errorf("load credentials from secret: %w", err)
        }
        h.creds = combinedCreds
        lambdaClient.SetCredentials(combinedCreds)
        logger.WithField("vault_count", len(combinedCreds.Vaults)).Info("Successfully loaded combined credentials")
    } else {
        logger.Warn("OBSYNC_SECRET_NAME not set, no credentials loaded")
    }

    return h, nil
}

func (h *Handler) ProcessEvent(ctx context.Context, event Event) (Response, error) {
	start := time.Now()
	
	h.logger.WithFields(map[string]interface{}{
		"action":    event.Action,
		"vault_id":  event.VaultID,
		"sync_type": event.SyncType,
	}).Info("Processing Lambda event")
	
	switch event.Action {
	case "sync":
		return h.handleSync(ctx, event, start)
	default:
		return Response{
			Success: false,
			Message: fmt.Sprintf("Unknown action: %s", event.Action),
		}, nil
	}
}

func (h *Handler) handleSync(ctx context.Context, event Event, start time.Time) (Response, error) {
	var vaultsSynced []string
	var totalFiles int
	var errors []string
	
	// Authenticate once at the beginning
	if h.creds != nil {
		h.logger.Info("Authenticating with Obsidian API")
		
		if err := h.client.Auth.Login(ctx, h.creds.Auth.Email, h.creds.Auth.Password, h.creds.Auth.TOTPSecret); err != nil {
			h.logger.WithError(err).Error("Authentication failed")
			return Response{
				Success: false,
				Message: "Failed to authenticate",
				Errors:  []string{fmt.Sprintf("Auth error: %v", err)},
			}, nil
		}
		h.logger.Info("Authentication successful")
	} else {
		return Response{
			Success: false,
			Message: "No credentials available",
			Errors:  []string{"Combined credentials not loaded"},
		}, nil
	}
	
	if event.VaultID != "" {
		// Sync single vault
		count, err := h.syncVault(ctx, event.VaultID, event.SyncType == "complete")
		if err != nil {
			errors = append(errors, fmt.Sprintf("vault %s: %v", event.VaultID, err))
		} else {
			vaultsSynced = append(vaultsSynced, event.VaultID)
			totalFiles = count
		}
	} else {
		// Sync all vaults
		h.logger.Info("Listing vaults")
		vaults, err := h.client.Vaults.ListVaults(ctx)
		if err != nil {
			h.logger.WithError(err).Error("Failed to list vaults")
			return Response{
				Success: false,
				Message: "Failed to list vaults",
				Errors:  []string{fmt.Sprintf("List vaults error: %v", err)},
			}, nil
		}
		h.logger.WithField("vault_count", len(vaults)).Info("Found vaults")
		
		for _, vault := range vaults {
			count, err := h.syncVault(ctx, vault.ID, event.SyncType == "complete")
			if err != nil {
				errors = append(errors, fmt.Sprintf("vault %s: %v", vault.ID, err))
				continue
			}
			vaultsSynced = append(vaultsSynced, vault.ID)
			totalFiles += count
		}
	}
	
	success := len(errors) == 0
	message := fmt.Sprintf("Synced %d vaults with %d files", len(vaultsSynced), totalFiles)
	if !success {
		message = fmt.Sprintf("Sync completed with %d errors", len(errors))
	}
	
	return Response{
		Success:      success,
		Message:      message,
		VaultsSynced: vaultsSynced,
		FilesCount:   totalFiles,
		Errors:       errors,
		Metadata: map[string]string{
			"sync_type":      event.SyncType,
			"execution_time": time.Since(start).String(),
		},
	}, nil
}

func (h *Handler) syncVault(ctx context.Context, vaultID string, complete bool) (int, error) {

    // Get vault info to get the name
    vault, err := h.client.Vaults.GetVault(ctx, vaultID)
    if err != nil {
        return 0, fmt.Errorf("get vault info: %w", err)
    }
    
    // Check if we have credentials for this vault
    if h.creds != nil && h.creds.Vaults != nil {
        vaultsMap := make(map[string]interface{})
        if err := json.Unmarshal(h.creds.Vaults, &vaultsMap); err == nil {
            if _, ok := vaultsMap[vault.Name]; !ok {
                h.logger.WithField("vault", vault.Name).Warn("Skipping vault - no credentials found")
                return 0, fmt.Errorf("no credentials found for vault %s", vault.Name)
            }
        }
    }
    
    // Set storage base path with vault name for S3 organization
    // This will create structure like: vaults/VaultName/...
    if err := h.client.SetStorageBase(vault.Name); err != nil {
        h.logger.WithError(err).Warn("Failed to set storage base path")
    }
    
    h.logger.WithField("vault", vault.Name).Info("Starting vault sync")

    // Set combined credentials for sync (includes vault-specific passwords)
    h.client.Sync.SetCombinedCredentials(h.creds)

    // Run sync (incremental unless complete)
    opts := syncsvc.SyncOptions{Initial: complete}
    if err := h.client.Sync.SyncVault(ctx, vaultID, opts); err != nil {
        return 0, err
    }
    return 0, nil
}

func loadLambdaConfig() (*config.Config, error) {
	cfg := config.DefaultConfig()
	
	// Override with Lambda-specific settings
	cfg.Storage.DataDir = "/tmp/obsync"
	cfg.Storage.StateDir = "/tmp/obsync/state" 
	cfg.Storage.TempDir = "/tmp/obsync/temp"
	cfg.Storage.MaxFileSize = 400 * 1024 * 1024 // Leave headroom in /tmp
	
	// Optimize for Lambda's high bandwidth (configurable via env)
	maxConcurrent := 50 // High default for Lambda's excellent network
	if v := os.Getenv("OBSYNC_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConcurrent = n
		}
	}
	cfg.Sync.MaxConcurrent = maxConcurrent
	
	// Configurable chunk size
	chunkSize := 10 * 1024 * 1024 // Default 10MB for faster downloads
	if v := os.Getenv("OBSYNC_CHUNK_SIZE_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			chunkSize = n * 1024 * 1024
		}
	}
	cfg.Sync.ChunkSize = chunkSize
	
	return cfg, nil
}
