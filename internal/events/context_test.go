package events_test

import (
	"context"
	"testing"
	
	"github.com/stretchr/testify/assert"
	
	"github.com/TheMichaelB/obsync/internal/events"
)

func TestFromContext(t *testing.T) {
	ctx := context.Background()
	
	// Should return default logger when none in context
	logger := events.FromContext(ctx)
	assert.NotNil(t, logger)
}

func TestWithLogger(t *testing.T) {
	ctx := context.Background()
	logger := &events.Logger{}
	
	ctx = events.WithLogger(ctx, logger)
	retrieved := events.FromContext(ctx)
	
	assert.Equal(t, logger, retrieved)
}

func TestWithRequestID(t *testing.T) {
	ctx := context.Background()
	requestID := "req-123"
	
	ctx = events.WithRequestID(ctx, requestID)
	retrieved := events.GetRequestID(ctx)
	
	assert.Equal(t, requestID, retrieved)
	
	// Should also add to logger fields
	logger := events.FromContext(ctx)
	assert.NotNil(t, logger)
}

func TestWithVaultID(t *testing.T) {
	ctx := context.Background()
	vaultID := "vault-456"
	
	ctx = events.WithVaultID(ctx, vaultID)
	retrieved := events.GetVaultID(ctx)
	
	assert.Equal(t, vaultID, retrieved)
	
	// Should also add to logger fields
	logger := events.FromContext(ctx)
	assert.NotNil(t, logger)
}

func TestGetRequestIDEmpty(t *testing.T) {
	ctx := context.Background()
	id := events.GetRequestID(ctx)
	assert.Empty(t, id)
}

func TestGetVaultIDEmpty(t *testing.T) {
	ctx := context.Background()
	id := events.GetVaultID(ctx)
	assert.Empty(t, id)
}

func TestSetDefault(t *testing.T) {
	customLogger := &events.Logger{}
	events.SetDefault(customLogger)
	
	ctx := context.Background()
	retrieved := events.FromContext(ctx)
	
	assert.Equal(t, customLogger, retrieved)
}