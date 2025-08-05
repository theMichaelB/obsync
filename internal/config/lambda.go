package config

import (
	"os"
	"strconv"
	"time"
)

// LambdaConfig contains Lambda-specific settings
type LambdaConfig struct {
	MaxMemoryMB      int           `json:"max_memory_mb"`
	TimeoutBuffer    time.Duration `json:"timeout_buffer"`
	BatchSize        int           `json:"batch_size"`
	MaxConcurrent    int           `json:"max_concurrent"`
	EnableProgress   bool          `json:"enable_progress"`
	S3Bucket         string        `json:"s3_bucket"`
	S3Prefix         string        `json:"s3_prefix"`
	StateTableName   string        `json:"state_table_name"`
}

// LoadLambdaConfig loads configuration for Lambda environment
func LoadLambdaConfig() *LambdaConfig {
	cfg := &LambdaConfig{
		MaxMemoryMB:   1024,
		TimeoutBuffer: 30 * time.Second,
		BatchSize:     100,
		MaxConcurrent: 5,
		EnableProgress: true,
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
	
	cfg.S3Bucket = os.Getenv("S3_BUCKET")
	cfg.S3Prefix = os.Getenv("S3_PREFIX")
	cfg.StateTableName = os.Getenv("STATE_TABLE_NAME")
	
	if cfg.StateTableName == "" {
		cfg.StateTableName = "obsync-state"
	}
	
	return cfg
}

// IsLambdaEnvironment checks if running in Lambda
func IsLambdaEnvironment() bool {
	return os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""
}