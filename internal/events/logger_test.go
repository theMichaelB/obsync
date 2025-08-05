package events_test

import (
	"bytes"
	"testing"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	"github.com/TheMichaelB/obsync/internal/config"
	"github.com/TheMichaelB/obsync/internal/events"
)

func TestNewLogger(t *testing.T) {
	cfg := &config.LogConfig{
		Level:  "debug",
		Format: "json",
		File:   "",
	}
	
	logger, err := events.NewLogger(cfg)
	require.NoError(t, err)
	assert.NotNil(t, logger)
}

func TestLoggerWithField(t *testing.T) {
	var buf bytes.Buffer
	cfg := &config.LogConfig{
		Level:  "info",
		Format: "json",
	}
	
	_, err := events.NewLogger(cfg)
	require.NoError(t, err)
	
	// Create test logger
	logger := events.NewTestLogger(events.InfoLevel, "json", &buf)
	
	logger.WithField("test_key", "test_value").Info("test message")
	
	output := buf.String()
	assert.Contains(t, output, `"test_key":"test_value"`)
	assert.Contains(t, output, `"msg":"test message"`)
	assert.Contains(t, output, `"level":"info"`)
}

func TestLoggerWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.InfoLevel, "json", &buf)
	
	fields := map[string]interface{}{
		"vault_id": "vault-123",
		"user_id":  "user-456",
	}
	
	logger.WithFields(fields).Info("multi-field test")
	
	output := buf.String()
	assert.Contains(t, output, `"vault_id":"vault-123"`)
	assert.Contains(t, output, `"user_id":"user-456"`)
	assert.Contains(t, output, `"msg":"multi-field test"`)
}

func TestLoggerLevels(t *testing.T) {
	tests := []struct {
		name      string
		logLevel  events.LogLevel
		msgLevel  events.LogLevel
		shouldLog bool
	}{
		{"debug logger, debug message", events.DebugLevel, events.DebugLevel, true},
		{"debug logger, info message", events.DebugLevel, events.InfoLevel, true},
		{"info logger, debug message", events.InfoLevel, events.DebugLevel, false},
		{"info logger, info message", events.InfoLevel, events.InfoLevel, true},
		{"error logger, warn message", events.ErrorLevel, events.WarnLevel, false},
		{"error logger, error message", events.ErrorLevel, events.ErrorLevel, true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := events.NewTestLogger(tt.logLevel, "text", &buf)
			
			switch tt.msgLevel {
			case events.DebugLevel:
				logger.Debug("test debug")
			case events.InfoLevel:
				logger.Info("test info")
			case events.WarnLevel:
				logger.Warn("test warn")
			case events.ErrorLevel:
				logger.Error("test error")
			}
			
			if tt.shouldLog {
				assert.NotEmpty(t, buf.String())
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func TestLoggerTextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.InfoLevel, "text", &buf)
	
	logger.WithField("key", "value").Info("test message")
	
	output := buf.String()
	// Should contain timestamp, level, message, and field
	assert.Contains(t, output, "[INFO]")
	assert.Contains(t, output, "test message")
	assert.Contains(t, output, "key=value")
}

func TestLoggerWithError(t *testing.T) {
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.InfoLevel, "json", &buf)
	
	err := assert.AnError
	logger.WithError(err).Error("operation failed")
	
	output := buf.String()
	assert.Contains(t, output, `"error":"assert.AnError general error for testing"`)
	assert.Contains(t, output, `"msg":"operation failed"`)
	assert.Contains(t, output, `"level":"error"`)
}