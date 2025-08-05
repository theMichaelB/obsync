package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	"github.com/yourusername/obsync/internal/config"
)

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	
	assert.NotEmpty(t, cfg.API.BaseURL)
	assert.Positive(t, cfg.API.Timeout)
	assert.NotEmpty(t, cfg.Storage.DataDir)
	assert.Positive(t, cfg.Sync.ChunkSize)
	assert.Equal(t, "info", cfg.Log.Level)
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*config.Config)
		wantErr string
	}{
		{
			name: "valid config",
			modify: func(c *config.Config) {
				// No modifications
			},
			wantErr: "",
		},
		{
			name: "missing base URL",
			modify: func(c *config.Config) {
				c.API.BaseURL = ""
			},
			wantErr: "api.base_url is required",
		},
		{
			name: "invalid log level",
			modify: func(c *config.Config) {
				c.Log.Level = "invalid"
			},
			wantErr: "invalid log level",
		},
		{
			name: "negative timeout",
			modify: func(c *config.Config) {
				c.API.Timeout = -1
			},
			wantErr: "api.timeout must be positive",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			tt.modify(cfg)
			
			err := cfg.Validate()
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoaderEnv(t *testing.T) {
	// Set test environment
	os.Setenv("OBSYNC_API_BASE_URL", "https://test.example.com")
	os.Setenv("OBSYNC_API_TIMEOUT", "45s")
	os.Setenv("OBSYNC_LOG_LEVEL", "debug")
	os.Setenv("OBSYNC_SYNC_MAX_CONCURRENT", "10")
	defer func() {
		os.Unsetenv("OBSYNC_API_BASE_URL")
		os.Unsetenv("OBSYNC_API_TIMEOUT")
		os.Unsetenv("OBSYNC_LOG_LEVEL")
		os.Unsetenv("OBSYNC_SYNC_MAX_CONCURRENT")
	}()
	
	loader := config.NewLoader("")
	cfg, err := loader.Load()
	
	require.NoError(t, err)
	assert.Equal(t, "https://test.example.com", cfg.API.BaseURL)
	assert.Equal(t, 45*time.Second, cfg.API.Timeout)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, 10, cfg.Sync.MaxConcurrent)
}

func TestLoaderFile(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.json")
	
	configJSON := `{
		"api": {
			"base_url": "https://file.example.com"
		},
		"log": {
			"level": "warn",
			"format": "json"
		}
	}`
	
	err := os.WriteFile(configPath, []byte(configJSON), 0644)
	require.NoError(t, err)
	
	loader := config.NewLoader(configPath)
	cfg, err := loader.Load()
	
	require.NoError(t, err)
	assert.Equal(t, "https://file.example.com", cfg.API.BaseURL)
	assert.Equal(t, "warn", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
}

func TestConfigEnsureDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Storage.DataDir = filepath.Join(tmpDir, "data")
	cfg.Storage.StateDir = filepath.Join(tmpDir, "data", "state")
	cfg.Storage.TempDir = filepath.Join(tmpDir, "data", "temp")
	cfg.Log.File = filepath.Join(tmpDir, "logs", "app.log")
	
	err := cfg.EnsureDirectories()
	require.NoError(t, err)
	
	// Check directories were created
	assert.DirExists(t, cfg.Storage.DataDir)
	assert.DirExists(t, cfg.Storage.StateDir)
	assert.DirExists(t, cfg.Storage.TempDir)
	assert.DirExists(t, filepath.Dir(cfg.Log.File))
}