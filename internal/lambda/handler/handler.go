package handler

import (
    "context"
    "fmt"
    "os"
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
	logCfg := &config.LogConfig{
		Level:  "info",
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
        combinedCreds, err := creds.LoadFromSecret(context.Background(), v)
        if err != nil {
            logger.WithError(err).Warn("Failed to load combined secret; continuing without")
        } else {
            h.creds = combinedCreds
            lambdaClient.SetCredentials(combinedCreds)
        }
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
		vaults, err := h.client.Vaults.ListVaults(ctx)
		if err != nil {
			return Response{
				Success: false,
				Message: "Failed to list vaults",
				Errors:  []string{err.Error()},
			}, nil
		}
		
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
    h.logger.WithField("vault_id", vaultID).Info("Starting vault sync")

    // Ensure we have combined credentials
    if h.creds == nil {
        return 0, fmt.Errorf("missing combined credentials")
    }

    // Login using combined credentials
    if err := h.client.Auth.Login(ctx, h.creds.Auth.Email, h.creds.Auth.Password, h.creds.Auth.TOTPSecret); err != nil {
        return 0, fmt.Errorf("login failed: %w", err)
    }

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
	
	return cfg, nil
}
