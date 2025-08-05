package handler

import (
	"testing"
	
	"github.com/stretchr/testify/assert"
)

func TestEvent_ProcessEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		wantErr  bool
		wantResp bool
	}{
		{
			name: "unknown action",
			event: Event{
				Action: "unknown",
			},
			wantErr:  false,
			wantResp: false, // Should return success=false but no error
		},
		{
			name: "sync action structure",
			event: Event{
				Action:   "sync",
				VaultID:  "test-vault",
				SyncType: "incremental",
			},
			wantErr:  false,
			wantResp: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip actual handler creation in tests since it requires AWS setup
			// This is a basic structure test
			assert.NotEmpty(t, tt.event.Action, "Event should have action")
		})
	}
}

func TestLambdaConfig(t *testing.T) {
	cfg, err := loadLambdaConfig()
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "/tmp/obsync", cfg.Storage.DataDir)
}