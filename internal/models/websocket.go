package models

import (
	"encoding/json"
	"time"
)

// WSMessageType defines WebSocket message types.
type WSMessageType string

const (
	// Client to Server
	WSTypeInit      WSMessageType = "init"
	WSTypeHeartbeat WSMessageType = "heartbeat"
	
	// Server to Client
	WSTypeInitResponse WSMessageType = "init_response"
	WSTypeFile        WSMessageType = "file"
	WSTypeFolder      WSMessageType = "folder"
	WSTypeDelete      WSMessageType = "delete"
	WSTypeDone        WSMessageType = "done"
	WSTypeError       WSMessageType = "error"
	WSTypePong        WSMessageType = "pong"
)

// WSMessage is the base WebSocket message structure.
type WSMessage struct {
	Type       WSMessageType   `json:"type"`
	UID        int             `json:"uid,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
	Data       json.RawMessage `json:"data"`
	
	// Binary message support
	IsBinary   bool   `json:"-"`  // Not serialized - internal flag
	BinaryData []byte `json:"-"`  // Not serialized - raw binary data
}

// InitMessage sent by client to start sync (matches Obsidian protocol).
type InitMessage struct {
	Op                string `json:"op"`                // "init"
	Token             string `json:"token"`             // auth token
	ID                string `json:"id"`                // vault_id
	Keyhash           string `json:"keyhash"`           // hex-encoded hash of vault key
	Version           int    `json:"version"`           // Last synced UID
	Initial           bool   `json:"initial"`           // Full vs incremental sync
	Device            string `json:"device"`            // Client identifier
	EncryptionVersion int    `json:"encryption_version"` // Vault encryption version
}

// InitResponse from server after init.
type InitResponse struct {
	Success       bool   `json:"success"`
	TotalFiles    int    `json:"total_files"`
	StartVersion  int    `json:"start_version"`
	LatestVersion int    `json:"latest_version"`
	Message       string `json:"message,omitempty"`
}

// FileMessage for file sync events.
type FileMessage struct {
	UID          int       `json:"uid"`
	Path         string    `json:"path"`          // Encrypted hex path
	Hash         string    `json:"hash"`          // SHA-256 of plaintext
	Size         int64     `json:"size"`          // Encrypted size
	ModifiedTime time.Time `json:"modified_time"`
	Deleted      bool      `json:"deleted"`
	ChunkID      string    `json:"chunk_id,omitempty"` // For download
}

// FolderMessage for directory creation.
type FolderMessage struct {
	UID  int    `json:"uid"`
	Path string `json:"path"` // Encrypted hex path
}

// DeleteMessage for file/folder deletion.
type DeleteMessage struct {
	UID    int    `json:"uid"`
	Path   string `json:"path"`
	IsFile bool   `json:"is_file"`
}

// DoneMessage signals sync completion.
type DoneMessage struct {
	FinalVersion int    `json:"final_version"`
	TotalSynced  int    `json:"total_synced"`
	Duration     string `json:"duration"`
}

// ErrorMessage for sync errors.
type ErrorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Fatal   bool   `json:"fatal"`
}