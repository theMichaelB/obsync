package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	creds "github.com/TheMichaelB/obsync/internal/creds"
	"github.com/TheMichaelB/obsync/internal/services/sync"
)

var syncCmd = &cobra.Command{
	Use:   "sync <vault-id>",
	Short: "Synchronize a vault to local filesystem",
	Long: `Sync downloads vault contents to the specified destination.
    
The sync is incremental by default, downloading only changed files
since the last sync. Use --full to force a complete sync.`,
	Example: `  obsync sync vault-123 --dest /path/to/vault
  obsync sync vault-123 --dest ./my-vault --full`,
	Args: cobra.ExactArgs(1),
	RunE: runSync,
}

var (
	syncDest     string
	syncFull     bool
	syncPassword string
	syncResume   bool
	syncDryRun   bool
)

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().StringVarP(&syncDest, "dest", "d", "",
		"Destination directory (required)")
	syncCmd.Flags().BoolVarP(&syncFull, "full", "f", false,
		"Force full sync instead of incremental")
	syncCmd.Flags().StringVarP(&syncPassword, "password", "p", "",
		"Vault password (will prompt if not provided)")
	syncCmd.Flags().BoolVar(&syncResume, "resume", true,
		"Resume from last sync position")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false,
		"Show what would be synced without downloading")

	_ = syncCmd.MarkFlagRequired("dest")
}

func runSync(cmd *cobra.Command, args []string) error {
	vaultID := args[0]
	ctx := context.Background()

	// Ensure authenticated
	if err := apiClient.Auth.EnsureAuthenticated(ctx); err != nil {
		return fmt.Errorf("not authenticated: %w", err)
	}

	// Get vault password (flag > combined file > legacy config > prompt)
	if syncPassword == "" {
		// Combined credentials file (preferred)
		if cfg != nil && cfg.Auth.CombinedCredentialsFile != "" {
			if c, err := creds.LoadFromFile(cfg.Auth.CombinedCredentialsFile); err == nil {
				if pw := c.VaultPassword(vaultID); pw != "" {
					syncPassword = pw
				}
				// If not authenticated yet and auth present, login automatically
				if token, _ := apiClient.Auth.GetToken(); token == nil || token.Token == "" || token.IsExpired() {
					if c.Auth.Email != "" && c.Auth.Password != "" {
						_ = apiClient.Auth.Login(ctx, c.Auth.Email, c.Auth.Password, c.Auth.TOTPSecret)
					}
				}
			}
		}
		// Legacy: inline map in config
		if syncPassword == "" && cfg != nil && cfg.Auth.VaultCredentials != nil {
			if pw, ok := cfg.Auth.VaultCredentials[vaultID]; ok && pw != "" {
				syncPassword = pw
			}
		}
		// Legacy: separate credentials file
		if syncPassword == "" && cfg != nil && cfg.Auth.VaultCredentialsFile != "" {
			if pw, err := lookupVaultPasswordFromFile(cfg.Auth.VaultCredentialsFile, vaultID); err == nil && pw != "" {
				syncPassword = pw
			}
		}
		// Prompt as last resort
		if syncPassword == "" {
			tokenInfo, _ := apiClient.Auth.GetToken()
			prompt := fmt.Sprintf("Vault password for %s: ", tokenInfo.Email)

			var err error
			syncPassword, err = promptPassword(prompt)
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
		}
	}

	// Resolve destination path
	destPath, err := filepath.Abs(syncDest)
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}

	// Create destination if needed
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("create destination: %w", err)
	}

	// Set up signal handling
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		printWarning("\nSync interrupted, cancelling...")
		cancel()
	}()

	// Configure sync
	if err := apiClient.SetStorageBase(destPath); err != nil {
		return fmt.Errorf("set storage base: %w", err)
	}
	tokenInfo, _ := apiClient.Auth.GetToken()
	apiClient.Sync.SetCredentials(tokenInfo.Email, syncPassword)

	// Start sync with progress display
	if jsonOutput {
		return runSyncJSON(ctx, vaultID)
	}
	return runSyncInteractive(ctx, vaultID)
}

// lookupVaultPasswordFromFile attempts to read a vault password from a JSON file.
// Supported formats:
// 1) { "vaults": { "<vault-id>": { "password": "..." } } }
// 2) { "<vault-id>": "password" }
func lookupVaultPasswordFromFile(path, vaultID string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// Try nested format first
	var nested struct {
		Vaults map[string]struct {
			Password string `json:"password"`
		} `json:"vaults"`
	}
	if err := json.Unmarshal(data, &nested); err == nil && nested.Vaults != nil {
		if v, ok := nested.Vaults[vaultID]; ok {
			return v.Password, nil
		}
	}

	// Try flat map format
	var flat map[string]string
	if err := json.Unmarshal(data, &flat); err == nil {
		if pw, ok := flat[vaultID]; ok {
			return pw, nil
		}
	}
	return "", nil
}

func runSyncInteractive(ctx context.Context, vaultID string) error {
	// Create progress display
	progress := NewProgressDisplay()
	defer progress.Close()

	// Monitor events
	go func() {
		for event := range apiClient.Sync.Events() {
			switch event.Type {
			case sync.EventStarted:
				progress.SetPhase("Initializing...")

			case sync.EventProgress:
				if event.Progress != nil {
					progress.Update(
						event.Progress.ProcessedFiles,
						event.Progress.TotalFiles,
						event.Progress.CurrentFile,
					)
				}

			case sync.EventFileComplete:
				logger.WithField("path", event.File.Path).Debug("File synced")

			case sync.EventFileError:
				progress.AddError(fmt.Sprintf("%s: %v", event.File.Path, event.Error))

			case sync.EventCompleted:
				progress.SetPhase("Completed")

			case sync.EventFailed:
				progress.SetPhase("Failed")
				if event.Error != nil {
					printError("Sync failed: %v", event.Error)
				}
			}
		}
	}()

	// Run sync
	opts := sync.SyncOptions{
		Initial: syncFull || !syncResume,
	}

	startTime := time.Now()
	err := apiClient.Sync.SyncVault(ctx, vaultID, opts)
	duration := time.Since(startTime)

	// Final summary
	finalProgress := apiClient.Sync.GetProgress()
	if finalProgress != nil {
		fmt.Printf("\n📊 Sync Summary:\n")
		fmt.Printf("   Files processed: %d/%d\n",
			finalProgress.ProcessedFiles, finalProgress.TotalFiles)
		fmt.Printf("   Data downloaded: %s\n",
			formatBytes(finalProgress.BytesDownloaded))
		fmt.Printf("   Duration: %s\n", duration.Round(time.Second))

		if len(finalProgress.Errors) > 0 {
			fmt.Printf("   Errors: %d\n", len(finalProgress.Errors))
		}
	}

	if err != nil {
		return err
	}

	printSuccess("\n✅ Sync completed successfully!")
	return nil
}

func runSyncJSON(ctx context.Context, vaultID string) error {
	// Collect all events
	var events []map[string]interface{}

	go func() {
		for event := range apiClient.Sync.Events() {
			eventData := map[string]interface{}{
				"type":      event.Type,
				"timestamp": event.Timestamp,
			}

			if event.File != nil {
				eventData["file"] = event.File
			}
			if event.Error != nil {
				eventData["error"] = event.Error.Error()
			}
			if event.Progress != nil {
				eventData["progress"] = event.Progress
			}

			events = append(events, eventData)
		}
	}()

	// Run sync
	opts := sync.SyncOptions{
		Initial: syncFull || !syncResume,
	}

	err := apiClient.Sync.SyncVault(ctx, vaultID, opts)

	// Output result
	result := map[string]interface{}{
		"success":  err == nil,
		"vault_id": vaultID,
		"events":   events,
	}

	if err != nil {
		result["error"] = err.Error()
	}

	printJSON(result)
	return err
}
