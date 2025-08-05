# Phase 3: Configuration & Logging

This phase implements secure configuration management and structured logging with proper error handling.

## Configuration System

### Config Structure

`internal/config/config.go`:

```go
package config

import (
    "encoding/json"
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
}

// DevConfig for development/debugging.
type DevConfig struct {
    SaveWebSocketTrace bool   `json:"save_websocket_trace"`
    TracePath          string `json:"trace_path"`
    InsecureSkipVerify bool   `json:"insecure_skip_verify"`
}

// DefaultConfig returns config with sensible defaults.
func DefaultConfig() *Config {
    homeDir, _ := os.UserHomeDir()
    dataDir := filepath.Join(homeDir, ".obsync")
    
    return &Config{
        API: APIConfig{
            BaseURL:    "https://api.obsync.app",
            Timeout:    30 * time.Second,
            MaxRetries: 3,
            UserAgent:  "obsync/1.0",
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
```

### Config Loading

`internal/config/loader.go`:

```go
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
```

## Structured Logging

### Logger Implementation

`internal/events/logger.go`:

```go
package events

import (
    "context"
    "fmt"
    "io"
    "os"
    "runtime"
    "strings"
    "sync"
    "time"
    
    "github.com/yourusername/obsync/internal/config"
)

// LogLevel represents logging severity.
type LogLevel int

const (
    DebugLevel LogLevel = iota
    InfoLevel
    WarnLevel
    ErrorLevel
)

// Logger provides structured logging.
type Logger struct {
    mu       sync.Mutex
    level    LogLevel
    format   string
    output   io.Writer
    fields   map[string]interface{}
    hostname string
}

// NewLogger creates a logger from config.
func NewLogger(cfg *config.LogConfig) (*Logger, error) {
    level := parseLevel(cfg.Level)
    
    var output io.Writer = os.Stdout
    if cfg.File != "" {
        // TODO: Add log rotation
        file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
        if err != nil {
            return nil, fmt.Errorf("open log file: %w", err)
        }
        output = file
    }
    
    hostname, _ := os.Hostname()
    
    return &Logger{
        level:    level,
        format:   cfg.Format,
        output:   output,
        fields:   make(map[string]interface{}),
        hostname: hostname,
    }, nil
}

// WithField returns a logger with an additional field.
func (l *Logger) WithField(key string, value interface{}) *Logger {
    l.mu.Lock()
    defer l.mu.Unlock()
    
    newFields := make(map[string]interface{}, len(l.fields)+1)
    for k, v := range l.fields {
        newFields[k] = v
    }
    newFields[key] = value
    
    return &Logger{
        level:    l.level,
        format:   l.format,
        output:   l.output,
        fields:   newFields,
        hostname: l.hostname,
    }
}

// WithFields returns a logger with additional fields.
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
    l.mu.Lock()
    defer l.mu.Unlock()
    
    newFields := make(map[string]interface{}, len(l.fields)+len(fields))
    for k, v := range l.fields {
        newFields[k] = v
    }
    for k, v := range fields {
        newFields[k] = v
    }
    
    return &Logger{
        level:    l.level,
        format:   l.format,
        output:   l.output,
        fields:   newFields,
        hostname: l.hostname,
    }
}

// WithError adds an error field.
func (l *Logger) WithError(err error) *Logger {
    return l.WithField("error", err.Error())
}

// Debug logs at debug level.
func (l *Logger) Debug(msg string) {
    l.log(DebugLevel, msg)
}

// Info logs at info level.
func (l *Logger) Info(msg string) {
    l.log(InfoLevel, msg)
}

// Warn logs at warn level.
func (l *Logger) Warn(msg string) {
    l.log(WarnLevel, msg)
}

// Error logs at error level.
func (l *Logger) Error(msg string) {
    l.log(ErrorLevel, msg)
}

// log writes a log entry.
func (l *Logger) log(level LogLevel, msg string) {
    if level < l.level {
        return
    }
    
    l.mu.Lock()
    defer l.mu.Unlock()
    
    entry := l.buildEntry(level, msg)
    
    if l.format == "json" {
        l.writeJSON(entry)
    } else {
        l.writeText(entry)
    }
}

// buildEntry creates a log entry.
func (l *Logger) buildEntry(level LogLevel, msg string) map[string]interface{} {
    // Get caller info
    _, file, line, _ := runtime.Caller(3)
    if idx := strings.LastIndex(file, "/"); idx >= 0 {
        file = file[idx+1:]
    }
    
    entry := map[string]interface{}{
        "time":     time.Now().UTC().Format(time.RFC3339Nano),
        "level":    levelString(level),
        "msg":      msg,
        "hostname": l.hostname,
        "caller":   fmt.Sprintf("%s:%d", file, line),
    }
    
    // Add custom fields
    for k, v := range l.fields {
        entry[k] = v
    }
    
    return entry
}

// writeJSON outputs JSON format.
func (l *Logger) writeJSON(entry map[string]interface{}) {
    // Manual JSON construction for performance
    var sb strings.Builder
    sb.WriteString("{")
    
    first := true
    for k, v := range entry {
        if !first {
            sb.WriteString(",")
        }
        first = false
        
        sb.WriteString(fmt.Sprintf(`"%s":`, k))
        
        switch val := v.(type) {
        case string:
            sb.WriteString(fmt.Sprintf(`"%s"`, escapeJSON(val)))
        case int, int64, float64:
            sb.WriteString(fmt.Sprintf("%v", val))
        case bool:
            sb.WriteString(fmt.Sprintf("%v", val))
        default:
            sb.WriteString(fmt.Sprintf(`"%v"`, val))
        }
    }
    
    sb.WriteString("}\n")
    l.output.Write([]byte(sb.String()))
}

// writeText outputs human-readable format.
func (l *Logger) writeText(entry map[string]interface{}) {
    levelStr := strings.ToUpper(entry["level"].(string))
    
    // Color codes for terminals
    var levelColor string
    if isTerminal(l.output) {
        switch levelStr {
        case "DEBUG":
            levelColor = "\033[36m" // Cyan
        case "INFO":
            levelColor = "\033[32m" // Green
        case "WARN":
            levelColor = "\033[33m" // Yellow
        case "ERROR":
            levelColor = "\033[31m" // Red
        }
    }
    
    // Format: TIME [LEVEL] Message key=value key=value
    fmt.Fprintf(l.output, "%s %s[%s]\033[0m %s",
        entry["time"],
        levelColor,
        levelStr,
        entry["msg"],
    )
    
    // Add fields
    for k, v := range entry {
        if k == "time" || k == "level" || k == "msg" || k == "hostname" || k == "caller" {
            continue
        }
        fmt.Fprintf(l.output, " %s=%v", k, v)
    }
    
    fmt.Fprintln(l.output)
}

// Helper functions

func parseLevel(s string) LogLevel {
    switch strings.ToLower(s) {
    case "debug":
        return DebugLevel
    case "warn":
        return WarnLevel
    case "error":
        return ErrorLevel
    default:
        return InfoLevel
    }
}

func levelString(l LogLevel) string {
    switch l {
    case DebugLevel:
        return "debug"
    case InfoLevel:
        return "info"
    case WarnLevel:
        return "warn"
    case ErrorLevel:
        return "error"
    default:
        return "unknown"
    }
}

func escapeJSON(s string) string {
    s = strings.ReplaceAll(s, `\`, `\\`)
    s = strings.ReplaceAll(s, `"`, `\"`)
    s = strings.ReplaceAll(s, "\n", `\n`)
    s = strings.ReplaceAll(s, "\r", `\r`)
    s = strings.ReplaceAll(s, "\t", `\t`)
    return s
}

func isTerminal(w io.Writer) bool {
    if f, ok := w.(*os.File); ok {
        return isatty(f.Fd())
    }
    return false
}

// isatty is a simple terminal check
func isatty(fd uintptr) bool {
    return fd == 1 || fd == 2 // stdout or stderr
}
```

### Context-Aware Logging

`internal/events/context.go`:

```go
package events

import (
    "context"
)

type contextKey int

const (
    loggerKey contextKey = iota
    requestIDKey
    userIDKey
    vaultIDKey
)

// FromContext extracts logger from context.
func FromContext(ctx context.Context) *Logger {
    if l, ok := ctx.Value(loggerKey).(*Logger); ok {
        return l
    }
    // Return default logger
    return defaultLogger
}

// WithLogger adds logger to context.
func WithLogger(ctx context.Context, logger *Logger) context.Context {
    return context.WithValue(ctx, loggerKey, logger)
}

// WithRequestID adds request ID to context.
func WithRequestID(ctx context.Context, id string) context.Context {
    logger := FromContext(ctx).WithField("request_id", id)
    ctx = context.WithValue(ctx, requestIDKey, id)
    return WithLogger(ctx, logger)
}

// WithVaultID adds vault ID to context.
func WithVaultID(ctx context.Context, id string) context.Context {
    logger := FromContext(ctx).WithField("vault_id", id)
    ctx = context.WithValue(ctx, vaultIDKey, id)
    return WithLogger(ctx, logger)
}

// GetRequestID retrieves request ID from context.
func GetRequestID(ctx context.Context) string {
    if id, ok := ctx.Value(requestIDKey).(string); ok {
        return id
    }
    return ""
}

// GetVaultID retrieves vault ID from context.
func GetVaultID(ctx context.Context) string {
    if id, ok := ctx.Value(vaultIDKey).(string); ok {
        return id
    }
    return ""
}

var defaultLogger = &Logger{
    level:  InfoLevel,
    format: "text",
    output: os.Stdout,
    fields: make(map[string]interface{}),
}

// SetDefault sets the default logger.
func SetDefault(logger *Logger) {
    defaultLogger = logger
}
```

## Usage Examples

### Configuration Usage

`cmd/obsync/main.go` update:

```go
package main

import (
    "context"
    "fmt"
    "os"
    
    "github.com/spf13/cobra"
    "github.com/yourusername/obsync/internal/config"
    "github.com/yourusername/obsync/internal/events"
)

var (
    cfg    *config.Config
    logger *events.Logger
)

func init() {
    cobra.OnInitialize(initConfig)
    
    rootCmd.PersistentFlags().String("config", "", "Config file path")
    rootCmd.PersistentFlags().String("log-level", "", "Log level (debug, info, warn, error)")
    rootCmd.PersistentFlags().String("log-format", "", "Log format (text, json)")
}

func initConfig() {
    configPath, _ := rootCmd.Flags().GetString("config")
    
    // Load configuration
    loader := config.NewLoader(configPath)
    var err error
    cfg, err = loader.Load()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
        os.Exit(1)
    }
    
    // Override from flags
    if level, _ := rootCmd.Flags().GetString("log-level"); level != "" {
        cfg.Log.Level = level
    }
    if format, _ := rootCmd.Flags().GetString("log-format"); format != "" {
        cfg.Log.Format = format
    }
    
    // Create directories
    if err := cfg.EnsureDirectories(); err != nil {
        fmt.Fprintf(os.Stderr, "Failed to create directories: %v\n", err)
        os.Exit(1)
    }
    
    // Initialize logger
    logger, err = events.NewLogger(&cfg.Log)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
        os.Exit(1)
    }
    
    events.SetDefault(logger)
    
    logger.WithFields(map[string]interface{}{
        "version": version,
        "commit":  commit,
        "config":  configPath,
    }).Info("Obsync initialized")
}
```

### Logging in Services

`internal/services/sync/service.go` example:

```go
package sync

import (
    "context"
    "fmt"
    "time"
    
    "github.com/yourusername/obsync/internal/events"
    "github.com/yourusername/obsync/internal/models"
)

type Service struct {
    // ... other fields
}

func (s *Service) Sync(ctx context.Context, vaultID string) error {
    logger := events.FromContext(ctx).
        WithField("vault_id", vaultID).
        WithField("operation", "sync")
    
    logger.Info("Starting sync")
    startTime := time.Now()
    
    // Track metrics
    defer func() {
        duration := time.Since(startTime)
        logger.WithField("duration_ms", duration.Milliseconds()).
            Info("Sync completed")
    }()
    
    // Load state
    state, err := s.stateStore.Load(vaultID)
    if err != nil {
        logger.WithError(err).Error("Failed to load sync state")
        return fmt.Errorf("load state: %w", err)
    }
    
    logger.WithFields(map[string]interface{}{
        "last_version": state.Version,
        "file_count":   len(state.Files),
    }).Debug("Loaded sync state")
    
    // ... sync implementation
    
    return nil
}

func (s *Service) handleFile(ctx context.Context, msg *models.FileMessage) error {
    logger := events.FromContext(ctx).
        WithFields(map[string]interface{}{
            "uid":  msg.UID,
            "path": msg.Path,
            "size": msg.Size,
        })
    
    logger.Debug("Processing file")
    
    // Decrypt path
    plainPath, err := s.crypto.DecryptPath(msg.Path, s.vaultKey)
    if err != nil {
        logger.WithError(err).Error("Failed to decrypt path")
        return &models.DecryptError{
            Path:   msg.Path,
            Reason: "path decryption",
            Err:    err,
        }
    }
    
    logger.WithField("plain_path", plainPath).Debug("Path decrypted")
    
    // ... rest of implementation
    
    return nil
}
```

## Testing

`internal/config/config_test.go`:

```go
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
```

## Phase 3 Deliverables

1. ✅ Complete configuration structure with validation
2. ✅ Environment variable override system
3. ✅ Structured logging with JSON/text formats
4. ✅ Context-aware logging with request tracking
5. ✅ Log levels and field enrichment
6. ✅ Configuration examples and tests
7. ✅ Integration with cobra CLI

## Verification Commands

```bash
# Test configuration loading
OBSYNC_LOG_LEVEL=debug go run cmd/obsync/main.go --help

# Create example config
go run cmd/obsync/main.go config example > obsync.json

# Test logging formats
OBSYNC_LOG_FORMAT=json go run cmd/obsync/main.go status
OBSYNC_LOG_FORMAT=text go run cmd/obsync/main.go status

# Run tests
go test -v ./internal/config/...
go test -v ./internal/events/...
```

## Next Steps

With configuration and logging in place, proceed to [Phase 4: Cryptography](phase-4-cryptography.md) to implement the AES-GCM encryption and key derivation.