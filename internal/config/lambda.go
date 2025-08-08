package config

import (
	"os"
	"strconv"
	"time"
)

// LambdaConfig contains Lambda-specific settings
type LambdaConfig struct {
	MaxMemoryMB        int           `json:"max_memory_mb"`
	TimeoutBuffer      time.Duration `json:"timeout_buffer"`
	BatchSize          int           `json:"batch_size"`
	MaxConcurrent      int           `json:"max_concurrent"`
	EnableProgress     bool          `json:"enable_progress"`
	S3Bucket           string        `json:"s3_bucket"`
	S3Prefix           string        `json:"s3_prefix"`
    S3StatePrefix      string        `json:"s3_state_prefix"`
    DownloadOnStartup  bool          `json:"download_on_startup"`
    SecretName         string        `json:"secret_name"`
}

// LoadLambdaConfig loads configuration for Lambda environment
func LoadLambdaConfig() *LambdaConfig {
	cfg := &LambdaConfig{
		MaxMemoryMB:       1024,
		TimeoutBuffer:     30 * time.Second,
		BatchSize:         100,
		MaxConcurrent:     5,
		EnableProgress:    true,
		DownloadOnStartup: true, // Default to downloading states on startup
	}
	
	// Override from environment
	if v := os.Getenv("LAMBDA_MAX_MEMORY_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxMemoryMB = n
		}
	}
	
	if v := os.Getenv("LAMBDA_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.BatchSize = n
		}
	}
	
	if v := os.Getenv("LAMBDA_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxConcurrent = n
		}
	}
	
	if v := os.Getenv("LAMBDA_DOWNLOAD_ON_STARTUP"); v != "" {
		cfg.DownloadOnStartup = v == "true" || v == "1"
	}
	
    cfg.S3Bucket = os.Getenv("S3_BUCKET")
    cfg.S3Prefix = os.Getenv("S3_PREFIX")
    cfg.S3StatePrefix = os.Getenv("S3_STATE_PREFIX")
    // Either name or ARN is acceptable for Secrets Manager
    if v := os.Getenv("OBSYNC_SECRET_NAME"); v != "" {
        cfg.SecretName = v
    }
	
	if cfg.S3StatePrefix == "" {
		cfg.S3StatePrefix = "state/"
	}
	
	return cfg
}

// IsLambdaEnvironment checks if running in Lambda
func IsLambdaEnvironment() bool {
	return os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""
}
