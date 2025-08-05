package events

import (
	"context"
	"os"
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