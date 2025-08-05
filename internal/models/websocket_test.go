package models_test

import (
	"encoding/json"
	"testing"
	"time"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	"github.com/yourusername/obsync/internal/models"
)

func TestParseWSMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *models.WSMessage
		wantErr bool
	}{
		{
			name: "valid file message",
			input: `{
				"type": "file",
				"uid": 123,
				"timestamp": "2024-01-15T10:00:00Z",
				"data": {"uid": 123, "path": "test"}
			}`,
			want: &models.WSMessage{
				Type:      models.WSTypeFile,
				UID:       123,
				Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			},
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := models.ParseWSMessage([]byte(tt.input))
			
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			
			require.NoError(t, err)
			assert.Equal(t, tt.want.Type, got.Type)
			assert.Equal(t, tt.want.UID, got.UID)
			assert.Equal(t, tt.want.Timestamp.Unix(), got.Timestamp.Unix())
		})
	}
}

func TestParseMessageData(t *testing.T) {
	tests := []struct {
		name    string
		msg     *models.WSMessage
		want    interface{}
		wantErr bool
	}{
		{
			name: "parse init response",
			msg: &models.WSMessage{
				Type: models.WSTypeInitResponse,
				Data: json.RawMessage(`{
					"success": true,
					"total_files": 42,
					"start_version": 0,
					"latest_version": 150
				}`),
			},
			want: &models.InitResponse{
				Success:       true,
				TotalFiles:    42,
				StartVersion:  0,
				LatestVersion: 150,
			},
		},
		{
			name: "parse file message",
			msg: &models.WSMessage{
				Type: models.WSTypeFile,
				Data: json.RawMessage(`{
					"uid": 123,
					"path": "6e6f7465732f746573742e6d64",
					"hash": "abc123",
					"size": 1024,
					"modified_time": "2024-01-15T10:00:00Z"
				}`),
			},
			want: &models.FileMessage{
				UID:          123,
				Path:         "6e6f7465732f746573742e6d64",
				Hash:         "abc123",
				Size:         1024,
				ModifiedTime: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "parse folder message",
			msg: &models.WSMessage{
				Type: models.WSTypeFolder,
				Data: json.RawMessage(`{
					"uid": 102,
					"path": "6e6f7465732f666f6c646572"
				}`),
			},
			want: &models.FolderMessage{
				UID:  102,
				Path: "6e6f7465732f666f6c646572",
			},
		},
		{
			name: "parse delete message",
			msg: &models.WSMessage{
				Type: models.WSTypeDelete,
				Data: json.RawMessage(`{
					"uid": 103,
					"path": "6f6c642f66696c652e6d64",
					"is_file": true
				}`),
			},
			want: &models.DeleteMessage{
				UID:    103,
				Path:   "6f6c642f66696c652e6d64",
				IsFile: true,
			},
		},
		{
			name: "parse done message",
			msg: &models.WSMessage{
				Type: models.WSTypeDone,
				Data: json.RawMessage(`{
					"final_version": 150,
					"total_synced": 42,
					"duration": "10.5s"
				}`),
			},
			want: &models.DoneMessage{
				FinalVersion: 150,
				TotalSynced:  42,
				Duration:     "10.5s",
			},
		},
		{
			name: "parse error message",
			msg: &models.WSMessage{
				Type: models.WSTypeError,
				Data: json.RawMessage(`{
					"code": "DECRYPTION_ERROR",
					"message": "Failed to decrypt",
					"fatal": true
				}`),
			},
			want: &models.ErrorMessage{
				Code:    "DECRYPTION_ERROR",
				Message: "Failed to decrypt",
				Fatal:   true,
			},
		},
		{
			name: "unknown message type",
			msg: &models.WSMessage{
				Type: "unknown",
				Data: json.RawMessage(`{}`),
			},
			wantErr: true,
		},
		{
			name: "invalid JSON in file message",
			msg: &models.WSMessage{
				Type: models.WSTypeFile,
				Data: json.RawMessage(`{invalid}`),
			},
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := models.ParseMessageData(tt.msg)
			
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}