package models_test

import (
	"testing"
	"time"
	
	"github.com/stretchr/testify/assert"
	
	"github.com/TheMichaelB/obsync/internal/models"
)

func TestFileItem_NormalizedPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "unix path",
			path: "notes/folder/file.md",
			want: "notes/folder/file.md",
		},
		{
			name: "windows path",
			path: "notes\\folder\\file.md",
			want: "notes/folder/file.md",
		},
		{
			name: "path with dot segments",
			path: "notes/../other/./file.md",
			want: "other/file.md",
		},
		{
			name: "root file",
			path: "file.md",
			want: "file.md",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := &models.FileItem{Path: tt.path}
			got := file.NormalizedPath()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFileEventType_Constants(t *testing.T) {
	// Verify constants are defined correctly
	assert.Equal(t, models.FileEventType("create"), models.FileEventCreate)
	assert.Equal(t, models.FileEventType("update"), models.FileEventUpdate)
	assert.Equal(t, models.FileEventType("delete"), models.FileEventDelete)
	assert.Equal(t, models.FileEventType("move"), models.FileEventMove)
	assert.Equal(t, models.FileEventType("error"), models.FileEventError)
}

func TestFileEvent_Structure(t *testing.T) {
	now := time.Now()
	event := &models.FileEvent{
		Type:      models.FileEventCreate,
		UID:       123,
		File:      &models.FileItem{Path: "test.md"},
		Timestamp: now,
	}
	
	assert.Equal(t, models.FileEventCreate, event.Type)
	assert.Equal(t, 123, event.UID)
	assert.Equal(t, "test.md", event.File.Path)
	assert.Equal(t, now, event.Timestamp)
}