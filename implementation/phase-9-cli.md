# Phase 9: CLI Interface

This phase implements the command-line interface using Cobra, with structured output, error formatting, and progress display.

## CLI Architecture

The CLI provides:
- Login with email/password and optional TOTP
- Vault listing and selection
- Full and incremental sync with progress
- Status display and state management
- Configuration commands
- Structured output (text/JSON)

## Root Command

`cmd/obsync/root.go`:

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"
    
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
    
    "github.com/TheMichaelB/obsync/internal/client"
    "github.com/TheMichaelB/obsync/internal/config"
    "github.com/TheMichaelB/obsync/internal/events"
)

var (
    // Set via ldflags
    version = "dev"
    commit  = "none"
    date    = "unknown"
    
    // Global flags
    cfgFile     string
    logLevel    string
    logFormat   string
    jsonOutput  bool
    noColor     bool
    
    // Global instances
    cfg       *config.Config
    logger    *events.Logger
    apiClient *client.Client
)

var rootCmd = &cobra.Command{
    Use:   "obsync",
    Short: "Secure one-way synchronization for Obsidian vaults",
    Long: `Obsync is a deterministic, secure synchronization tool that replicates
Obsidian vaults from a remote service to the local filesystem.

It supports encrypted content, incremental sync, and resumable state.`,
    Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
    PersistentPreRunE: initializeApp,
}

func init() {
    cobra.OnInitialize(initConfig)
    
    // Global flags
    rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", 
        "Config file (default: $HOME/.obsync/config.json)")
    rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", 
        "Log level (debug, info, warn, error)")
    rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "", 
        "Log format (text, json)")
    rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, 
        "Output in JSON format")
    rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, 
        "Disable color output")
    
    // Bind to viper
    viper.BindPFlag("log.level", rootCmd.PersistentFlags().Lookup("log-level"))
    viper.BindPFlag("log.format", rootCmd.PersistentFlags().Lookup("log-format"))
}

func initConfig() {
    if cfgFile != "" {
        viper.SetConfigFile(cfgFile)
    } else {
        home, err := os.UserHomeDir()
        cobra.CheckErr(err)
        
        viper.AddConfigPath(filepath.Join(home, ".obsync"))
        viper.AddConfigPath(".")
        viper.SetConfigName("config")
        viper.SetConfigType("json")
    }
    
    viper.SetEnvPrefix("OBSYNC")
    viper.AutomaticEnv()
    
    if err := viper.ReadInConfig(); err == nil {
        if !jsonOutput {
            fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
        }
    }
}

func initializeApp(cmd *cobra.Command, args []string) error {
    // Skip initialization for certain commands
    if cmd.Name() == "version" || cmd.Name() == "help" {
        return nil
    }
    
    // Load configuration
    var err error
    cfg = &config.Config{}
    if err = viper.Unmarshal(cfg); err != nil {
        return fmt.Errorf("parse config: %w", err)
    }
    
    // Apply defaults
    if cfg.API.BaseURL == "" {
        cfg.API.BaseURL = config.DefaultConfig().API.BaseURL
    }
    
    // Override from flags
    if logLevel != "" {
        cfg.Log.Level = logLevel
    }
    if logFormat != "" {
        cfg.Log.Format = logFormat
    }
    
    // Create logger
    logger, err = events.NewLogger(&cfg.Log)
    if err != nil {
        return fmt.Errorf("create logger: %w", err)
    }
    
    // Create API client
    apiClient, err = client.New(cfg, logger)
    if err != nil {
        return fmt.Errorf("create client: %w", err)
    }
    
    return nil
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

## Login Command

`cmd/obsync/login.go`:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "os"
    "strings"
    "syscall"
    
    "github.com/spf13/cobra"
    "golang.org/x/term"
)

var loginCmd = &cobra.Command{
    Use:   "login",
    Short: "Authenticate with the Obsync service",
    Long:  `Login stores authentication credentials for future sync operations.`,
    Example: `  obsync login --email user@example.com
  obsync login --email user@example.com --totp 123456`,
    RunE: runLogin,
}

var (
    loginEmail    string
    loginPassword string
    loginTOTP     string
)

func init() {
    rootCmd.AddCommand(loginCmd)
    
    loginCmd.Flags().StringVarP(&loginEmail, "email", "e", "", 
        "Email address (required)")
    loginCmd.Flags().StringVarP(&loginPassword, "password", "p", "", 
        "Password (will prompt if not provided)")
    loginCmd.Flags().StringVar(&loginTOTP, "totp", "", 
        "TOTP code for 2FA")
    
    loginCmd.MarkFlagRequired("email")
}

func runLogin(cmd *cobra.Command, args []string) error {
    ctx := context.Background()
    
    // Get password if not provided
    if loginPassword == "" {
        var err error
        loginPassword, err = promptPassword("Password: ")
        if err != nil {
            return fmt.Errorf("read password: %w", err)
        }
    }
    
    // Attempt login
    if err := apiClient.Auth.Login(ctx, loginEmail, loginPassword, loginTOTP); err != nil {
        if jsonOutput {
            printJSON(map[string]interface{}{
                "success": false,
                "error":   err.Error(),
            })
        } else {
            printError("Login failed: %v", err)
        }
        return err
    }
    
    // Success
    if jsonOutput {
        printJSON(map[string]interface{}{
            "success": true,
            "email":   loginEmail,
        })
    } else {
        printSuccess("Successfully logged in as %s", loginEmail)
    }
    
    return nil
}

func promptPassword(prompt string) (string, error) {
    fmt.Fprint(os.Stderr, prompt)
    
    // Read password without echo
    password, err := term.ReadPassword(int(syscall.Stdin))
    fmt.Fprintln(os.Stderr) // New line after password
    
    if err != nil {
        return "", err
    }
    
    return string(password), nil
}
```

## Vault Commands

`cmd/obsync/vaults.go`:

```go
package main

import (
    "context"
    "fmt"
    "os"
    "text/tabwriter"
    
    "github.com/spf13/cobra"
)

var vaultsCmd = &cobra.Command{
    Use:   "vaults",
    Short: "List available vaults",
    Long:  `Display all vaults accessible with current credentials.`,
    RunE:  runVaults,
}

func init() {
    rootCmd.AddCommand(vaultsCmd)
}

func runVaults(cmd *cobra.Command, args []string) error {
    ctx := context.Background()
    
    // Ensure authenticated
    if err := apiClient.Auth.EnsureAuthenticated(ctx); err != nil {
        return fmt.Errorf("not authenticated: %w", err)
    }
    
    // Fetch vaults
    vaults, err := apiClient.Vaults.ListVaults(ctx)
    if err != nil {
        if jsonOutput {
            printJSON(map[string]interface{}{
                "success": false,
                "error":   err.Error(),
            })
        } else {
            printError("Failed to list vaults: %v", err)
        }
        return err
    }
    
    // Display results
    if jsonOutput {
        printJSON(map[string]interface{}{
            "success": true,
            "vaults":  vaults,
        })
    } else {
        if len(vaults) == 0 {
            fmt.Println("No vaults found.")
            return nil
        }
        
        // Table output
        w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
        fmt.Fprintln(w, "ID\tNAME\tENCRYPTION")
        fmt.Fprintln(w, "──\t────\t──────────")
        
        for _, vault := range vaults {
            fmt.Fprintf(w, "%s\t%s\tv%d\n", 
                vault.ID, 
                vault.Name,
                vault.EncryptionInfo.EncryptionVersion,
            )
        }
        
        w.Flush()
    }
    
    return nil
}
```

## Sync Command

`cmd/obsync/sync.go`:

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "path/filepath"
    "strings"
    "time"
    
    "github.com/spf13/cobra"
    
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
    
    syncCmd.MarkFlagRequired("dest")
}

func runSync(cmd *cobra.Command, args []string) error {
    vaultID := args[0]
    ctx := context.Background()
    
    // Ensure authenticated
    if err := apiClient.Auth.EnsureAuthenticated(ctx); err != nil {
        return fmt.Errorf("not authenticated: %w", err)
    }
    
    // Get vault password
    if syncPassword == "" {
        tokenInfo, _ := apiClient.Auth.GetToken()
        prompt := fmt.Sprintf("Vault password for %s: ", tokenInfo.Email)
        
        var err error
        syncPassword, err = promptPassword(prompt)
        if err != nil {
            return fmt.Errorf("read password: %w", err)
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
    apiClient.SetStorageBase(destPath)
    apiClient.Sync.SetCredentials(tokenInfo.Email, syncPassword)
    
    // Start sync with progress display
    if jsonOutput {
        return runSyncJSON(ctx, vaultID)
    }
    return runSyncInteractive(ctx, vaultID)
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
        "success": err == nil,
        "vault_id": vaultID,
        "events":   events,
    }
    
    if err != nil {
        result["error"] = err.Error()
    }
    
    printJSON(result)
    return err
}
```

## Status Command

`cmd/obsync/status.go`:

```go
package main

import (
    "context"
    "fmt"
    "os"
    "text/tabwriter"
    "time"
    
    "github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
    Use:   "status",
    Short: "Show sync status for all vaults",
    Long:  `Display the current synchronization state of all configured vaults.`,
    RunE:  runStatus,
}

func init() {
    rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
    ctx := context.Background()
    
    // List all vault states
    states, err := apiClient.State.ListStates()
    if err != nil {
        return fmt.Errorf("list states: %w", err)
    }
    
    if jsonOutput {
        printJSON(map[string]interface{}{
            "success": true,
            "states":  states,
        })
        return nil
    }
    
    if len(states) == 0 {
        fmt.Println("No vaults have been synchronized yet.")
        return nil
    }
    
    // Display table
    w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
    fmt.Fprintln(w, "VAULT ID\tVERSION\tFILES\tLAST SYNC\tSTATUS")
    fmt.Fprintln(w, "────────\t───────\t─────\t─────────\t──────")
    
    for _, state := range states {
        status := "✅ OK"
        if state.LastError != "" {
            status = "❌ Error"
        }
        
        lastSync := "Never"
        if !state.LastSyncTime.IsZero() {
            lastSync = formatTime(state.LastSyncTime)
        }
        
        fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n",
            state.VaultID,
            state.Version,
            len(state.Files),
            lastSync,
            status,
        )
    }
    
    w.Flush()
    
    // Show any errors
    for _, state := range states {
        if state.LastError != "" {
            fmt.Printf("\n⚠️  %s: %s\n", state.VaultID, state.LastError)
        }
    }
    
    return nil
}

func formatTime(t time.Time) string {
    if time.Since(t) < 24*time.Hour {
        return t.Format("15:04 MST")
    }
    return t.Format("Jan 2, 2006")
}
```

## Reset Command

`cmd/obsync/reset.go`:

```go
package main

import (
    "bufio"
    "fmt"
    "os"
    "strings"
    
    "github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
    Use:   "reset [vault-id]",
    Short: "Reset sync state for a vault",
    Long: `Reset removes the local sync state for a vault, forcing a full sync
on the next synchronization. If no vault ID is provided, all states are reset.`,
    Example: `  obsync reset vault-123
  obsync reset --all`,
    RunE: runReset,
}

var resetAll bool
var resetForce bool

func init() {
    rootCmd.AddCommand(resetCmd)
    
    resetCmd.Flags().BoolVar(&resetAll, "all", false, 
        "Reset all vault states")
    resetCmd.Flags().BoolVarP(&resetForce, "force", "f", false, 
        "Skip confirmation prompt")
}

func runReset(cmd *cobra.Command, args []string) error {
    var vaultIDs []string
    
    if resetAll {
        states, err := apiClient.State.ListStates()
        if err != nil {
            return fmt.Errorf("list states: %w", err)
        }
        
        for _, state := range states {
            vaultIDs = append(vaultIDs, state.VaultID)
        }
    } else if len(args) > 0 {
        vaultIDs = args
    } else {
        return fmt.Errorf("specify vault ID or use --all")
    }
    
    if len(vaultIDs) == 0 {
        fmt.Println("No vault states to reset.")
        return nil
    }
    
    // Confirm
    if !resetForce && !jsonOutput {
        fmt.Printf("This will reset sync state for %d vault(s).\n", len(vaultIDs))
        fmt.Print("Continue? [y/N] ")
        
        reader := bufio.NewReader(os.Stdin)
        response, _ := reader.ReadString('\n')
        response = strings.TrimSpace(strings.ToLower(response))
        
        if response != "y" && response != "yes" {
            fmt.Println("Cancelled.")
            return nil
        }
    }
    
    // Reset states
    var errors []error
    for _, vaultID := range vaultIDs {
        if err := apiClient.State.Reset(vaultID); err != nil {
            errors = append(errors, fmt.Errorf("%s: %w", vaultID, err))
        } else if !jsonOutput {
            printSuccess("Reset state for %s", vaultID)
        }
    }
    
    if jsonOutput {
        printJSON(map[string]interface{}{
            "success": len(errors) == 0,
            "reset":   vaultIDs,
            "errors":  errors,
        })
    } else if len(errors) > 0 {
        for _, err := range errors {
            printError("Failed to reset: %v", err)
        }
        return fmt.Errorf("reset failed for %d vault(s)", len(errors))
    }
    
    return nil
}
```

## Progress Display

`cmd/obsync/progress.go`:

```go
package main

import (
    "fmt"
    "io"
    "os"
    "strings"
    "sync"
    "time"
    
    "github.com/vbauerster/mpb/v8"
    "github.com/vbauerster/mpb/v8/decor"
)

// ProgressDisplay shows sync progress.
type ProgressDisplay struct {
    progress *mpb.Progress
    bar      *mpb.Bar
    phase    string
    errors   []string
    mu       sync.Mutex
}

// NewProgressDisplay creates a progress display.
func NewProgressDisplay() *ProgressDisplay {
    var progress *mpb.Progress
    
    if noColor || os.Getenv("NO_COLOR") != "" {
        progress = mpb.New(mpb.WithOutput(os.Stderr))
    } else {
        progress = mpb.New(
            mpb.WithOutput(os.Stderr),
            mpb.WithWidth(80),
        )
    }
    
    return &ProgressDisplay{
        progress: progress,
    }
}

// SetPhase updates the current phase.
func (pd *ProgressDisplay) SetPhase(phase string) {
    pd.mu.Lock()
    defer pd.mu.Unlock()
    
    pd.phase = phase
    
    if pd.bar == nil {
        pd.bar = pd.progress.New(100,
            mpb.BarStyle().Lbound("[").Filler("=").Tip(">").Padding("-").Rbound("]"),
            mpb.PrependDecorators(
                decor.Name(phase, decor.WC{W: 20, C: decor.DidentRight}),
                decor.CountersNoUnit("%d/%d", decor.WCSyncWidth),
            ),
            mpb.AppendDecorators(
                decor.Percentage(decor.WC{W: 5}),
                decor.Name(" "),
                decor.AverageSpeed(decor.UnitKB, "% .1f", decor.WCSyncWidth),
            ),
        )
    } else {
        pd.bar.SetRefill(0)
    }
}

// Update updates progress.
func (pd *ProgressDisplay) Update(current, total int, currentFile string) {
    pd.mu.Lock()
    defer pd.mu.Unlock()
    
    if pd.bar == nil {
        pd.SetPhase("Syncing")
    }
    
    pd.bar.SetTotal(int64(total), false)
    pd.bar.SetCurrent(int64(current))
    
    // Show current file
    if currentFile != "" {
        name := currentFile
        if len(name) > 40 {
            name = "..." + name[len(name)-37:]
        }
        pd.bar.DecoratorSet(mpb.PrependDecorators(
            decor.Name(name, decor.WC{W: 40, C: decor.DidentRight}),
            decor.CountersNoUnit("%d/%d", decor.WCSyncWidth),
        ))
    }
}

// AddError adds an error message.
func (pd *ProgressDisplay) AddError(err string) {
    pd.mu.Lock()
    defer pd.mu.Unlock()
    
    pd.errors = append(pd.errors, err)
    
    // Show inline if not too many
    if len(pd.errors) <= 3 {
        fmt.Fprintf(os.Stderr, "\r\033[K⚠️  %s\n", err)
    }
}

// Close finalizes the display.
func (pd *ProgressDisplay) Close() {
    pd.progress.Wait()
    
    // Show all errors if many
    if len(pd.errors) > 3 {
        fmt.Fprintf(os.Stderr, "\n⚠️  %d errors occurred:\n", len(pd.errors))
        for i, err := range pd.errors {
            fmt.Fprintf(os.Stderr, "   %d. %s\n", i+1, err)
        }
    }
}
```

## Output Helpers

`cmd/obsync/output.go`:

```go
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "strings"
    
    "github.com/fatih/color"
)

var (
    colorSuccess = color.New(color.FgGreen)
    colorError   = color.New(color.FgRed)
    colorWarning = color.New(color.FgYellow)
    colorInfo    = color.New(color.FgCyan)
)

func init() {
    if noColor || os.Getenv("NO_COLOR") != "" {
        color.NoColor = true
    }
}

func printSuccess(format string, args ...interface{}) {
    if !jsonOutput {
        colorSuccess.Fprintf(os.Stderr, "✅ "+format+"\n", args...)
    }
}

func printError(format string, args ...interface{}) {
    if !jsonOutput {
        colorError.Fprintf(os.Stderr, "❌ "+format+"\n", args...)
    }
}

func printWarning(format string, args ...interface{}) {
    if !jsonOutput {
        colorWarning.Fprintf(os.Stderr, "⚠️  "+format+"\n", args...)
    }
}

func printInfo(format string, args ...interface{}) {
    if !jsonOutput {
        colorInfo.Fprintf(os.Stderr, "ℹ️  "+format+"\n", args...)
    }
}

func printJSON(data interface{}) {
    encoder := json.NewEncoder(os.Stdout)
    encoder.SetIndent("", "  ")
    encoder.Encode(data)
}

func formatBytes(bytes int64) string {
    const unit = 1024
    if bytes < unit {
        return fmt.Sprintf("%d B", bytes)
    }
    
    div, exp := int64(unit), 0
    for n := bytes / unit; n >= unit; n /= unit {
        div *= unit
        exp++
    }
    
    return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatDuration(d time.Duration) string {
    if d < time.Minute {
        return fmt.Sprintf("%.1fs", d.Seconds())
    }
    if d < time.Hour {
        return fmt.Sprintf("%.1fm", d.Minutes())
    }
    return fmt.Sprintf("%.1fh", d.Hours())
}
```

## Config Command

`cmd/obsync/config.go`:

```go
package main

import (
    "fmt"
    "os"
    
    "github.com/spf13/cobra"
    
    "github.com/TheMichaelB/obsync/internal/config"
)

var configCmd = &cobra.Command{
    Use:   "config",
    Short: "Manage configuration",
    Long:  `View and manage Obsync configuration settings.`,
}

var configShowCmd = &cobra.Command{
    Use:   "show",
    Short: "Show current configuration",
    RunE:  runConfigShow,
}

var configExampleCmd = &cobra.Command{
    Use:   "example",
    Short: "Generate example configuration",
    RunE:  runConfigExample,
}

func init() {
    rootCmd.AddCommand(configCmd)
    configCmd.AddCommand(configShowCmd)
    configCmd.AddCommand(configExampleCmd)
}

func runConfigShow(cmd *cobra.Command, args []string) error {
    if jsonOutput {
        printJSON(cfg)
    } else {
        // Pretty print config
        fmt.Println("Current Configuration:")
        fmt.Println("─────────────────────")
        fmt.Printf("API URL:        %s\n", cfg.API.BaseURL)
        fmt.Printf("Timeout:        %s\n", cfg.API.Timeout)
        fmt.Printf("Max Retries:    %d\n", cfg.API.MaxRetries)
        fmt.Printf("\n")
        fmt.Printf("Data Directory: %s\n", cfg.Storage.DataDir)
        fmt.Printf("State Directory: %s\n", cfg.Storage.StateDir)
        fmt.Printf("Max File Size:  %s\n", formatBytes(cfg.Storage.MaxFileSize))
        fmt.Printf("\n")
        fmt.Printf("Log Level:      %s\n", cfg.Log.Level)
        fmt.Printf("Log Format:     %s\n", cfg.Log.Format)
        
        if cfgFile != "" {
            fmt.Printf("\nConfig File:    %s\n", cfgFile)
        }
    }
    
    return nil
}

func runConfigExample(cmd *cobra.Command, args []string) error {
    example := config.DefaultConfig()
    
    if jsonOutput {
        printJSON(example)
    } else {
        encoder := json.NewEncoder(os.Stdout)
        encoder.SetIndent("", "  ")
        encoder.Encode(example)
    }
    
    return nil
}
```

## Phase 9 Deliverables

1. ✅ Complete CLI structure with Cobra
2. ✅ Login command with secure password input
3. ✅ Vault listing with formatted output
4. ✅ Sync command with progress display
5. ✅ Status and reset commands
6. ✅ JSON output mode for scripting
7. ✅ Colored output with emoji indicators
8. ✅ Signal handling for graceful shutdown
9. ✅ Configuration management commands

## Verification Commands

```bash
# Build CLI
go build -o obsync ./cmd/obsync

# Test commands
./obsync --help
./obsync login --help
./obsync sync --help

# Test with JSON output
./obsync --json vaults
./obsync --json status

# Test configuration
./obsync config show
./obsync config example > config.json
```

## Auth Test Command

`cmd/obsync/auth-test.go`:

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/spf13/cobra"
)

var authTestCmd = &cobra.Command{
    Use:   "auth-test",
    Short: "Test authentication components",
    Long:  `Utilities for testing individual authentication steps.`,
}

var authTestOTPCmd = &cobra.Command{
    Use:   "otp",
    Short: "Request OTP for email",
    Long:  `Request a one-time password for the specified email address.`,
    Example: `  obsync auth-test otp --email user@example.com`,
    RunE: runAuthTestOTP,
}

var authTestTokenCmd = &cobra.Command{
    Use:   "token",
    Short: "Exchange OTP for access token",
    Long:  `Exchange email and OTP for an access token.`,
    Example: `  obsync auth-test token --email user@example.com --otp 123456`,
    RunE: runAuthTestToken,
}

var authTestVerifyCmd = &cobra.Command{
    Use:   "verify",
    Short: "Verify current token",
    Long:  `Check if the stored access token is valid.`,
    RunE: runAuthTestVerify,
}

var (
    authTestEmail string
    authTestOTP   string
)

func init() {
    rootCmd.AddCommand(authTestCmd)
    authTestCmd.AddCommand(authTestOTPCmd)
    authTestCmd.AddCommand(authTestTokenCmd)
    authTestCmd.AddCommand(authTestVerifyCmd)
    
    // OTP flags
    authTestOTPCmd.Flags().StringVarP(&authTestEmail, "email", "e", "", 
        "Email address (required)")
    authTestOTPCmd.MarkFlagRequired("email")
    
    // Token flags
    authTestTokenCmd.Flags().StringVarP(&authTestEmail, "email", "e", "", 
        "Email address (required)")
    authTestTokenCmd.Flags().StringVar(&authTestOTP, "otp", "", 
        "OTP code (required)")
    authTestTokenCmd.MarkFlagRequired("email")
    authTestTokenCmd.MarkFlagRequired("otp")
}

func runAuthTestOTP(cmd *cobra.Command, args []string) error {
    ctx := context.Background()
    
    logger.WithField("email", authTestEmail).Info("Requesting OTP")
    
    startTime := time.Now()
    err := apiClient.Auth.RequestOTP(ctx, authTestEmail)
    duration := time.Since(startTime)
    
    if err != nil {
        if jsonOutput {
            printJSON(map[string]interface{}{
                "success": false,
                "email":   authTestEmail,
                "error":   err.Error(),
                "duration_ms": duration.Milliseconds(),
            })
        } else {
            printError("Failed to request OTP: %v", err)
            fmt.Printf("Duration: %v\n", duration)
        }
        return err
    }
    
    if jsonOutput {
        printJSON(map[string]interface{}{
            "success": true,
            "email":   authTestEmail,
            "message": "OTP sent",
            "duration_ms": duration.Milliseconds(),
        })
    } else {
        printSuccess("OTP sent to %s", authTestEmail)
        fmt.Printf("Duration: %v\n", duration)
    }
    
    return nil
}

func runAuthTestToken(cmd *cobra.Command, args []string) error {
    ctx := context.Background()
    
    logger.WithFields(map[string]interface{}{
        "email": authTestEmail,
        "otp":   authTestOTP,
    }).Info("Exchanging OTP for token")
    
    startTime := time.Now()
    token, err := apiClient.Auth.ExchangeOTP(ctx, authTestEmail, authTestOTP)
    duration := time.Since(startTime)
    
    if err != nil {
        if jsonOutput {
            printJSON(map[string]interface{}{
                "success": false,
                "email":   authTestEmail,
                "error":   err.Error(),
                "duration_ms": duration.Milliseconds(),
            })
        } else {
            printError("Failed to exchange OTP: %v", err)
            fmt.Printf("Duration: %v\n", duration)
        }
        return err
    }
    
    if jsonOutput {
        printJSON(map[string]interface{}{
            "success": true,
            "email":   authTestEmail,
            "token": map[string]interface{}{
                "access_token": token.AccessToken,
                "refresh_token": token.RefreshToken,
                "expires_at": token.ExpiresAt,
            },
            "duration_ms": duration.Milliseconds(),
        })
    } else {
        printSuccess("Token obtained successfully")
        fmt.Printf("Email: %s\n", authTestEmail)
        fmt.Printf("Access Token: %s...%s\n", 
            token.AccessToken[:8], 
            token.AccessToken[len(token.AccessToken)-8:])
        fmt.Printf("Expires: %s\n", token.ExpiresAt.Format(time.RFC3339))
        fmt.Printf("Duration: %v\n", duration)
    }
    
    return nil
}

func runAuthTestVerify(cmd *cobra.Command, args []string) error {
    ctx := context.Background()
    
    // Get current token
    tokenInfo, err := apiClient.Auth.GetToken()
    if err != nil {
        if jsonOutput {
            printJSON(map[string]interface{}{
                "success": false,
                "error":   "No token found",
            })
        } else {
            printError("No token found: %v", err)
        }
        return err
    }
    
    // Verify with server
    startTime := time.Now()
    valid, err := apiClient.Auth.VerifyToken(ctx)
    duration := time.Since(startTime)
    
    if err != nil {
        if jsonOutput {
            printJSON(map[string]interface{}{
                "success": false,
                "error":   err.Error(),
                "duration_ms": duration.Milliseconds(),
            })
        } else {
            printError("Token verification failed: %v", err)
            fmt.Printf("Duration: %v\n", duration)
        }
        return err
    }
    
    if jsonOutput {
        printJSON(map[string]interface{}{
            "success": true,
            "valid":   valid,
            "email":   tokenInfo.Email,
            "expires_at": tokenInfo.ExpiresAt,
            "duration_ms": duration.Milliseconds(),
        })
    } else {
        if valid {
            printSuccess("Token is valid")
        } else {
            printWarning("Token is invalid")
        }
        fmt.Printf("Email: %s\n", tokenInfo.Email)
        fmt.Printf("Expires: %s\n", tokenInfo.ExpiresAt.Format(time.RFC3339))
        fmt.Printf("Duration: %v\n", duration)
    }
    
    return nil
}
```

## Connection Test Command

`cmd/obsync/connection-test.go`:

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/spf13/cobra"
    
    "github.com/TheMichaelB/obsync/internal/models"
)

var connTestCmd = &cobra.Command{
    Use:   "conn-test",
    Short: "Test connectivity components",
    Long:  `Utilities for testing WebSocket connections and message flow.`,
}

var connTestWSCmd = &cobra.Command{
    Use:   "websocket <vault-id>",
    Short: "Test WebSocket connection",
    Long:  `Establish a WebSocket connection and test message exchange.`,
    Example: `  obsync conn-test websocket vault-123 --password mypass`,
    Args: cobra.ExactArgs(1),
    RunE: runConnTestWebSocket,
}

var connTestPingCmd = &cobra.Command{
    Use:   "ping",
    Short: "Test API connectivity",
    Long:  `Check if the API server is reachable and responsive.`,
    RunE: runConnTestPing,
}

var (
    connTestPassword string
    connTestDuration time.Duration
    connTestMessages int
)

func init() {
    rootCmd.AddCommand(connTestCmd)
    connTestCmd.AddCommand(connTestWSCmd)
    connTestCmd.AddCommand(connTestPingCmd)
    
    // WebSocket flags
    connTestWSCmd.Flags().StringVarP(&connTestPassword, "password", "p", "",
        "Vault password (will prompt if not provided)")
    connTestWSCmd.Flags().DurationVar(&connTestDuration, "duration", 10*time.Second,
        "Test duration")
    connTestWSCmd.Flags().IntVar(&connTestMessages, "messages", 5,
        "Number of test messages to send")
}

func runConnTestWebSocket(cmd *cobra.Command, args []string) error {
    vaultID := args[0]
    ctx := context.Background()
    
    // Ensure authenticated
    if err := apiClient.Auth.EnsureAuthenticated(ctx); err != nil {
        return fmt.Errorf("not authenticated: %w", err)
    }
    
    // Get vault password if needed
    if connTestPassword == "" {
        tokenInfo, _ := apiClient.Auth.GetToken()
        prompt := fmt.Sprintf("Vault password for %s: ", tokenInfo.Email)
        
        var err error
        connTestPassword, err = promptPassword(prompt)
        if err != nil {
            return fmt.Errorf("read password: %w", err)
        }
    }
    
    logger.WithField("vault_id", vaultID).Info("Testing WebSocket connection")
    
    // Create test context with timeout
    testCtx, cancel := context.WithTimeout(ctx, connTestDuration)
    defer cancel()
    
    // Connect
    startTime := time.Now()
    conn, err := apiClient.Transport.Connect(testCtx, vaultID)
    if err != nil {
        if jsonOutput {
            printJSON(map[string]interface{}{
                "success": false,
                "vault_id": vaultID,
                "error":   err.Error(),
                "duration_ms": time.Since(startTime).Milliseconds(),
            })
        } else {
            printError("Failed to connect: %v", err)
        }
        return err
    }
    defer conn.Close()
    
    connectDuration := time.Since(startTime)
    
    if !jsonOutput {
        printSuccess("WebSocket connected in %v", connectDuration)
        fmt.Printf("Vault ID: %s\n", vaultID)
        fmt.Printf("Testing for %v...\n\n", connTestDuration)
    }
    
    // Message tracking
    messagesSent := 0
    messagesReceived := 0
    errors := 0
    
    // Send test messages
    go func() {
        ticker := time.NewTicker(connTestDuration / time.Duration(connTestMessages))
        defer ticker.Stop()
        
        for {
            select {
            case <-testCtx.Done():
                return
            case <-ticker.C:
                if messagesSent >= connTestMessages {
                    return
                }
                
                msg := models.WebSocketMessage{
                    Type: models.MessageTypePing,
                    Timestamp: time.Now(),
                }
                
                if err := conn.SendMessage(msg); err != nil {
                    logger.WithError(err).Error("Failed to send ping")
                    errors++
                } else {
                    messagesSent++
                    if !jsonOutput {
                        fmt.Printf("→ Sent ping #%d\n", messagesSent)
                    }
                }
            }
        }
    }()
    
    // Receive messages
    for {
        select {
        case <-testCtx.Done():
            goto done
        default:
            msg, err := conn.ReadMessage()
            if err != nil {
                logger.WithError(err).Error("Failed to read message")
                errors++
                continue
            }
            
            messagesReceived++
            if !jsonOutput {
                fmt.Printf("← Received %s message\n", msg.Type)
            }
            
            // Log message details at debug level
            logger.WithFields(map[string]interface{}{
                "type": msg.Type,
                "timestamp": msg.Timestamp,
            }).Debug("Message received")
        }
    }
    
done:
    totalDuration := time.Since(startTime)
    
    // Results
    if jsonOutput {
        printJSON(map[string]interface{}{
            "success": true,
            "vault_id": vaultID,
            "connect_duration_ms": connectDuration.Milliseconds(),
            "total_duration_ms": totalDuration.Milliseconds(),
            "messages_sent": messagesSent,
            "messages_received": messagesReceived,
            "errors": errors,
        })
    } else {
        fmt.Printf("\n📊 Connection Test Summary:\n")
        fmt.Printf("   Connect time: %v\n", connectDuration)
        fmt.Printf("   Test duration: %v\n", totalDuration)
        fmt.Printf("   Messages sent: %d\n", messagesSent)
        fmt.Printf("   Messages received: %d\n", messagesReceived)
        fmt.Printf("   Errors: %d\n", errors)
        
        if errors == 0 {
            printSuccess("\n✅ WebSocket connection test passed!")
        } else {
            printWarning("\n⚠️  WebSocket test completed with errors")
        }
    }
    
    return nil
}

func runConnTestPing(cmd *cobra.Command, args []string) error {
    ctx := context.Background()
    
    logger.Info("Testing API connectivity")
    
    // Ping API
    startTime := time.Now()
    err := apiClient.Transport.Ping(ctx)
    duration := time.Since(startTime)
    
    if err != nil {
        if jsonOutput {
            printJSON(map[string]interface{}{
                "success": false,
                "error":   err.Error(),
                "duration_ms": duration.Milliseconds(),
            })
        } else {
            printError("API ping failed: %v", err)
            fmt.Printf("Duration: %v\n", duration)
        }
        return err
    }
    
    if jsonOutput {
        printJSON(map[string]interface{}{
            "success": true,
            "api_url": cfg.API.BaseURL,
            "duration_ms": duration.Milliseconds(),
        })
    } else {
        printSuccess("API is reachable")
        fmt.Printf("URL: %s\n", cfg.API.BaseURL)
        fmt.Printf("Response time: %v\n", duration)
    }
    
    return nil
}
```

## Debug Command

`cmd/obsync/debug.go`:

```go
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"
    
    "github.com/spf13/cobra"
)

var debugCmd = &cobra.Command{
    Use:   "debug",
    Short: "Debug utilities",
    Long:  `Tools for inspecting internal state and troubleshooting.`,
}

var debugTokenCmd = &cobra.Command{
    Use:   "token",
    Short: "Show stored authentication token",
    Long:  `Display the current stored access token and its metadata.`,
    RunE: runDebugToken,
}

var debugStateCmd = &cobra.Command{
    Use:   "state [vault-id]",
    Short: "Show vault sync state",
    Long:  `Display detailed sync state for a vault.`,
    RunE: runDebugState,
}

var debugConfigCmd = &cobra.Command{
    Use:   "config",
    Short: "Show resolved configuration",
    Long:  `Display the complete resolved configuration including defaults.`,
    RunE: runDebugConfig,
}

var (
    debugVerbose bool
    debugRaw     bool
)

func init() {
    rootCmd.AddCommand(debugCmd)
    debugCmd.AddCommand(debugTokenCmd)
    debugCmd.AddCommand(debugStateCmd)
    debugCmd.AddCommand(debugConfigCmd)
    
    debugCmd.PersistentFlags().BoolVarP(&debugVerbose, "verbose", "v", false,
        "Show verbose output")
    debugCmd.PersistentFlags().BoolVar(&debugRaw, "raw", false,
        "Show raw data without formatting")
}

func runDebugToken(cmd *cobra.Command, args []string) error {
    // Load token from storage
    tokenPath := filepath.Join(cfg.Storage.StateDir, "auth", "token.json")
    
    data, err := os.ReadFile(tokenPath)
    if err != nil {
        if os.IsNotExist(err) {
            if jsonOutput {
                printJSON(map[string]interface{}{
                    "success": false,
                    "error":   "No token found",
                })
            } else {
                printError("No token found at %s", tokenPath)
            }
            return err
        }
        return fmt.Errorf("read token: %w", err)
    }
    
    if debugRaw {
        fmt.Println(string(data))
        return nil
    }
    
    // Parse token
    var tokenData map[string]interface{}
    if err := json.Unmarshal(data, &tokenData); err != nil {
        return fmt.Errorf("parse token: %w", err)
    }
    
    if jsonOutput {
        printJSON(tokenData)
        return nil
    }
    
    // Format output
    fmt.Println("Stored Token Information:")
    fmt.Println("────────────────────────")
    
    if email, ok := tokenData["email"].(string); ok {
        fmt.Printf("Email: %s\n", email)
    }
    
    if accessToken, ok := tokenData["access_token"].(string); ok {
        if debugVerbose {
            fmt.Printf("Access Token: %s\n", accessToken)
        } else {
            fmt.Printf("Access Token: %s...%s\n", 
                accessToken[:8], 
                accessToken[len(accessToken)-8:])
        }
    }
    
    if refreshToken, ok := tokenData["refresh_token"].(string); ok && debugVerbose {
        fmt.Printf("Refresh Token: %s...%s\n", 
            refreshToken[:8], 
            refreshToken[len(refreshToken)-8:])
    }
    
    if expiresAt, ok := tokenData["expires_at"].(string); ok {
        if t, err := time.Parse(time.RFC3339, expiresAt); err == nil {
            fmt.Printf("Expires: %s (%s)\n", 
                t.Format(time.RFC3339),
                time.Until(t).Round(time.Second))
        }
    }
    
    fmt.Printf("\nToken File: %s\n", tokenPath)
    
    return nil
}

func runDebugState(cmd *cobra.Command, args []string) error {
    var vaultID string
    if len(args) > 0 {
        vaultID = args[0]
    }
    
    if vaultID == "" {
        // List all states
        states, err := apiClient.State.ListStates()
        if err != nil {
            return fmt.Errorf("list states: %w", err)
        }
        
        if jsonOutput {
            printJSON(map[string]interface{}{
                "success": true,
                "states":  states,
            })
            return nil
        }
        
        fmt.Println("Available Vault States:")
        fmt.Println("──────────────────────")
        for _, state := range states {
            fmt.Printf("- %s (v%d, %d files)\n", 
                state.VaultID, state.Version, len(state.Files))
        }
        
        if len(states) > 0 {
            fmt.Println("\nUse 'obsync debug state <vault-id>' for details")
        }
        
        return nil
    }
    
    // Load specific state
    state, err := apiClient.State.LoadState(vaultID)
    if err != nil {
        return fmt.Errorf("load state: %w", err)
    }
    
    if jsonOutput || debugRaw {
        printJSON(state)
        return nil
    }
    
    // Format output
    fmt.Printf("Vault State: %s\n", vaultID)
    fmt.Println("─────────────────────")
    fmt.Printf("Version: %d\n", state.Version)
    fmt.Printf("Last Sync: %s\n", state.LastSyncTime.Format(time.RFC3339))
    fmt.Printf("Files: %d\n", len(state.Files))
    
    if state.LastError != "" {
        fmt.Printf("Last Error: %s\n", state.LastError)
    }
    
    if debugVerbose && len(state.Files) > 0 {
        fmt.Println("\nFiles:")
        count := 0
        for path, info := range state.Files {
            fmt.Printf("  %s\n", path)
            fmt.Printf("    Hash: %s\n", info.Hash)
            fmt.Printf("    Size: %d bytes\n", info.Size)
            fmt.Printf("    Modified: %s\n", info.ModifiedTime.Format(time.RFC3339))
            
            count++
            if count >= 10 && !debugVerbose {
                fmt.Printf("  ... and %d more files\n", len(state.Files)-10)
                break
            }
        }
    }
    
    // State file location
    statePath := filepath.Join(cfg.Storage.StateDir, "vaults", vaultID+".json")
    fmt.Printf("\nState File: %s\n", statePath)
    
    return nil
}

func runDebugConfig(cmd *cobra.Command, args []string) error {
    if jsonOutput || debugRaw {
        printJSON(cfg)
        return nil
    }
    
    // Pretty print with all values
    fmt.Println("Resolved Configuration:")
    fmt.Println("──────────────────────")
    
    fmt.Println("\nAPI Settings:")
    fmt.Printf("  Base URL: %s\n", cfg.API.BaseURL)
    fmt.Printf("  Timeout: %s\n", cfg.API.Timeout)
    fmt.Printf("  Max Retries: %d\n", cfg.API.MaxRetries)
    fmt.Printf("  Rate Limit: %d req/s\n", cfg.API.RateLimit)
    
    fmt.Println("\nStorage Settings:")
    fmt.Printf("  Data Directory: %s\n", cfg.Storage.DataDir)
    fmt.Printf("  State Directory: %s\n", cfg.Storage.StateDir)
    fmt.Printf("  Temp Directory: %s\n", cfg.Storage.TempDir)
    fmt.Printf("  Max File Size: %s\n", formatBytes(cfg.Storage.MaxFileSize))
    fmt.Printf("  Buffer Size: %s\n", formatBytes(int64(cfg.Storage.BufferSize)))
    
    fmt.Println("\nSync Settings:")
    fmt.Printf("  Max Concurrent: %d\n", cfg.Sync.MaxConcurrent)
    fmt.Printf("  Max Batch Size: %d\n", cfg.Sync.MaxBatchSize)
    fmt.Printf("  Progress Interval: %s\n", cfg.Sync.ProgressInterval)
    fmt.Printf("  Retry Attempts: %d\n", cfg.Sync.RetryAttempts)
    fmt.Printf("  Retry Delay: %s\n", cfg.Sync.RetryDelay)
    
    fmt.Println("\nLogging Settings:")
    fmt.Printf("  Level: %s\n", cfg.Log.Level)
    fmt.Printf("  Format: %s\n", cfg.Log.Format)
    fmt.Printf("  Color: %v\n", cfg.Log.Color)
    fmt.Printf("  Timestamp: %v\n", cfg.Log.Timestamp)
    
    if cfg.Dev.Enabled {
        fmt.Println("\nDevelopment Mode:")
        fmt.Printf("  Enabled: true\n")
        fmt.Printf("  Mock Responses: %v\n", cfg.Dev.MockResponses)
        fmt.Printf("  Save Traces: %v\n", cfg.Dev.SaveWebSocketTrace)
        fmt.Printf("  Trace Path: %s\n", cfg.Dev.TracePath)
        fmt.Printf("  Slow Requests: %v\n", cfg.Dev.SlowRequests)
        fmt.Printf("  Fail Rate: %.2f\n", cfg.Dev.FailRate)
    }
    
    // Config sources
    fmt.Println("\nConfiguration Sources:")
    if cfgFile != "" {
        fmt.Printf("  Config File: %s\n", cfgFile)
    }
    fmt.Printf("  Environment: OBSYNC_* variables\n")
    fmt.Printf("  Defaults: Built-in\n")
    
    return nil
}
```

## Updated Init Function

Add the new commands to the root command initialization:

```go
// In cmd/obsync/root.go init() function, add:
func init() {
    // ... existing init code ...
    
    // Add all commands
    rootCmd.AddCommand(loginCmd)
    rootCmd.AddCommand(vaultsCmd)
    rootCmd.AddCommand(syncCmd)
    rootCmd.AddCommand(statusCmd)
    rootCmd.AddCommand(resetCmd)
    rootCmd.AddCommand(configCmd)
    rootCmd.AddCommand(authTestCmd)    // New
    rootCmd.AddCommand(connTestCmd)    // New
    rootCmd.AddCommand(debugCmd)       // New
}
```

## Example Usage

```bash
# Test authentication flow
obsync auth-test otp --email user@example.com
obsync auth-test token --email user@example.com --otp 123456
obsync auth-test verify

# Test connectivity
obsync conn-test ping
obsync conn-test websocket vault-123 --password mypass --duration 30s

# Debug information
obsync debug token
obsync debug state vault-123
obsync debug config --verbose

# JSON output for scripting
obsync --json auth-test verify
obsync --json conn-test ping
obsync --json debug state
```

## Next Steps

With the CLI complete, proceed to [Phase 10: Testing & Quality](phase-10-testing.md) to implement comprehensive testing, benchmarks, and integration tests.