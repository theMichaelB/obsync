package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Loader handles configuration loading from multiple sources.
type Loader struct {
	configPath string
	envPrefix  string
}

// NewLoader creates a config loader.
func NewLoader(configPath string) *Loader {
	return &Loader{
		configPath: configPath,
		envPrefix:  "OBSYNC_",
	}
}

// Load reads configuration from file and environment.
func (l *Loader) Load() (*Config, error) {
	// Start with defaults
	cfg := DefaultConfig()
	
	// Load from file if exists
	if l.configPath != "" {
		if err := l.loadFile(cfg); err != nil {
			return nil, fmt.Errorf("load config file: %w", err)
		}
	} else {
		// Try default locations
		for _, path := range l.defaultPaths() {
			if _, err := os.Stat(path); err == nil {
				l.configPath = path
				if err := l.loadFile(cfg); err != nil {
					return nil, fmt.Errorf("load config file %s: %w", path, err)
				}
				break
			}
		}
	}
	
	// Override with environment variables
	if err := l.loadEnv(cfg); err != nil {
		return nil, fmt.Errorf("load env config: %w", err)
	}
	
	// Validate final config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	
	return cfg, nil
}

// defaultPaths returns default config file locations.
func (l *Loader) defaultPaths() []string {
	paths := []string{
		"obsync.json",
		".obsync.json",
	}
	
	if homeDir, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(homeDir, ".config", "obsync", "config.json"),
			filepath.Join(homeDir, ".obsync", "config.json"),
		)
	}
	
	return paths
}

// loadFile reads config from JSON file.
func (l *Loader) loadFile(cfg *Config) error {
	data, err := os.ReadFile(l.configPath)
	if err != nil {
		return err
	}
	
	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	
	return nil
}

// loadEnv overrides config from environment variables.
func (l *Loader) loadEnv(cfg *Config) error {
	// API settings
	if v := os.Getenv(l.envPrefix + "API_BASE_URL"); v != "" {
		cfg.API.BaseURL = v
	}
	
	if v := os.Getenv(l.envPrefix + "API_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("parse API_TIMEOUT: %w", err)
		}
		cfg.API.Timeout = d
	}
	
	// Storage settings
	if v := os.Getenv(l.envPrefix + "DATA_DIR"); v != "" {
		cfg.Storage.DataDir = v
		// Update dependent paths
		cfg.Storage.StateDir = filepath.Join(v, "state")
		cfg.Storage.TempDir = filepath.Join(v, "temp")
	}
	
	// Sync settings
	if v := os.Getenv(l.envPrefix + "SYNC_MAX_CONCURRENT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parse SYNC_MAX_CONCURRENT: %w", err)
		}
		cfg.Sync.MaxConcurrent = n
	}
	
	// Log settings
	if v := os.Getenv(l.envPrefix + "LOG_LEVEL"); v != "" {
		cfg.Log.Level = strings.ToLower(v)
	}
	
	if v := os.Getenv(l.envPrefix + "LOG_FORMAT"); v != "" {
		cfg.Log.Format = strings.ToLower(v)
	}
	
	if v := os.Getenv(l.envPrefix + "LOG_FILE"); v != "" {
		cfg.Log.File = v
	}
	
	// Auth settings
	if v := os.Getenv(l.envPrefix + "EMAIL"); v != "" {
		cfg.Auth.Email = v
	}
	
	if v := os.Getenv(l.envPrefix + "PASSWORD"); v != "" {
		cfg.Auth.Password = v
	}
	
	if v := os.Getenv(l.envPrefix + "TOTP_SECRET"); v != "" {
		cfg.Auth.TOTPSecret = v
	}
	
	// Alternative environment variable names for compatibility
	if v := os.Getenv("OBSIDIAN_EMAIL"); v != "" {
		cfg.Auth.Email = v
	}
	
	if v := os.Getenv("OBSIDIAN_PASSWORD"); v != "" {
		cfg.Auth.Password = v
	}
	
	if v := os.Getenv("OBSIDIAN_TOTP_SECRET"); v != "" {
		cfg.Auth.TOTPSecret = v
	}
	
	// Dev settings
	if v := os.Getenv(l.envPrefix + "DEV_INSECURE"); v != "" {
		cfg.Dev.InsecureSkipVerify = v == "true" || v == "1"
	}
	
	return nil
}

// SaveExample writes an example config file.
func SaveExample(path string) error {
	cfg := DefaultConfig()
	
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	
	// Add comments
	example := fmt.Sprintf(`{
  // Obsync configuration file
  // Environment variables override these settings using OBSYNC_ prefix
  // For example: OBSYNC_LOG_LEVEL=debug
  
%s
}`, string(data)[1:len(data)-1])
	
	if err := os.WriteFile(path, []byte(example), 0600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	
	return nil
}