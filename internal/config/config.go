package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config holds all application configuration.
type Config struct {
	// API configuration
	API APIConfig `json:"api"`
	
	// Authentication configuration
	Auth AuthConfig `json:"auth"`
	
	// Storage paths
	Storage StorageConfig `json:"storage"`
	
	// Sync behavior
	Sync SyncConfig `json:"sync"`
	
	// Logging
	Log LogConfig `json:"log"`
	
	// Development options
	Dev DevConfig `json:"dev,omitempty"`
}

// APIConfig for server communication.
type APIConfig struct {
	BaseURL    string        `json:"base_url"`
	Timeout    time.Duration `json:"timeout"`
	MaxRetries int           `json:"max_retries"`
	UserAgent  string        `json:"user_agent"`
}

// AuthConfig for authentication settings.
type AuthConfig struct {
	// Obsidian account credentials
	Email    string `json:"email,omitempty"`
	Password string `json:"password,omitempty"`
	
	// TOTP/MFA configuration
	TOTPSecret string `json:"totp_secret,omitempty"`
	
	// Token persistence
	TokenFile string `json:"token_file"`
	
	// Vault credentials file path
	VaultCredentialsFile string `json:"vault_credentials_file"`
	
	// Alternative: inline vault credentials (not recommended for production)
	VaultCredentials map[string]string `json:"vault_credentials,omitempty"`
}

// StorageConfig for local file paths.
type StorageConfig struct {
	DataDir     string `json:"data_dir"`     // Base directory for all data
	StateDir    string `json:"state_dir"`    // Sync state storage
	TempDir     string `json:"temp_dir"`     // Temporary files
	MaxFileSize int64  `json:"max_file_size"` // Max file size in bytes
}

// SyncConfig for synchronization behavior.
type SyncConfig struct {
	ChunkSize         int           `json:"chunk_size"`          // Download chunk size
	MaxConcurrent     int           `json:"max_concurrent"`      // Concurrent downloads
	RetryAttempts     int           `json:"retry_attempts"`      // Per-file retry count
	RetryDelay        time.Duration `json:"retry_delay"`         // Initial retry delay
	ProgressInterval  time.Duration `json:"progress_interval"`   // Progress update frequency
	ValidateChecksums bool          `json:"validate_checksums"`  // Verify file hashes
}

// LogConfig for logging behavior.
type LogConfig struct {
	Level      string `json:"level"`       // debug, info, warn, error
	Format     string `json:"format"`      // text, json
	File       string `json:"file"`        // Log file path (empty = stdout)
	MaxSize    int    `json:"max_size"`    // Max log file size in MB
	MaxBackups int    `json:"max_backups"` // Max number of old logs
	MaxAge     int    `json:"max_age"`     // Max age in days
	Color      bool   `json:"color"`       // Enable colored output
	Timestamp  bool   `json:"timestamp"`   // Include timestamps
}

// DevConfig for development/debugging.
type DevConfig struct {
	SaveWebSocketTrace bool   `json:"save_websocket_trace"`
	TracePath          string `json:"trace_path"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
}

// DefaultConfig returns config with sensible defaults.
func DefaultConfig() *Config {
	dataDir := ".obsync"
	
	return &Config{
		API: APIConfig{
			BaseURL:    "https://api.obsidian.md",
			Timeout:    30 * time.Second,
			MaxRetries: 3,
			UserAgent:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) obsidian/1.8.10 Chrome/132.0.6834.196 Electron/34.2.0 Safari/537.36",
		},
		Storage: StorageConfig{
			DataDir:     dataDir,
			StateDir:    filepath.Join(dataDir, "state"),
			TempDir:     filepath.Join(dataDir, "temp"),
			MaxFileSize: 100 * 1024 * 1024, // 100MB
		},
		Sync: SyncConfig{
			ChunkSize:         1024 * 1024, // 1MB chunks
			MaxConcurrent:     5,
			RetryAttempts:     3,
			RetryDelay:        time.Second,
			ProgressInterval:  500 * time.Millisecond,
			ValidateChecksums: true,
		},
		Log: LogConfig{
			Level:      "info",
			Format:     "text",
			File:       "",
			MaxSize:    10,
			MaxBackups: 3,
			MaxAge:     7,
			Color:      true,
			Timestamp:  true,
		},
	}
}

// Validate checks configuration validity.
func (c *Config) Validate() error {
	if c.API.BaseURL == "" {
		return errors.New("api.base_url is required")
	}
	
	if c.API.Timeout <= 0 {
		return errors.New("api.timeout must be positive")
	}
	
	if c.Storage.MaxFileSize <= 0 {
		return errors.New("storage.max_file_size must be positive")
	}
	
	if c.Sync.ChunkSize <= 0 {
		return errors.New("sync.chunk_size must be positive")
	}
	
	if c.Sync.MaxConcurrent <= 0 {
		return errors.New("sync.max_concurrent must be positive")
	}
	
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLevels[c.Log.Level] {
		return fmt.Errorf("invalid log level: %s", c.Log.Level)
	}
	
	validFormats := map[string]bool{"text": true, "json": true}
	if !validFormats[c.Log.Format] {
		return fmt.Errorf("invalid log format: %s", c.Log.Format)
	}
	
	return nil
}

// EnsureDirectories creates required directories.
func (c *Config) EnsureDirectories() error {
	dirs := []string{
		c.Storage.DataDir,
		c.Storage.StateDir,
		c.Storage.TempDir,
	}
	
	if c.Log.File != "" {
		dirs = append(dirs, filepath.Dir(c.Log.File))
	}
	
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	
	return nil
}