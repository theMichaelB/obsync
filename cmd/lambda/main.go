package main

import (
	"context"
	"log"
	
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/TheMichaelB/obsync/internal/lambda/handler"
)

// Global handler instance for reuse across warm starts
var h *handler.Handler

func init() {
	// Initialize handler once during cold start
	var err error
	h, err = handler.NewHandler()
	if err != nil {
		log.Fatalf("Failed to initialize handler: %v", err)
	}
}

// Event represents the Lambda input event
type Event struct {
	// Core sync parameters
	Action   string `json:"action"`              // "sync"
	VaultID  string `json:"vault_id,omitempty"`  // Empty means all vaults
	SyncType string `json:"sync_type"`           // "complete" or "incremental"
	
	// S3 destination override (optional)
	DestBucket string `json:"dest_bucket,omitempty"`
	DestPrefix string `json:"dest_prefix,omitempty"`
}

// Response represents the Lambda response
type Response struct {
	Success      bool              `json:"success"`
	Message      string            `json:"message"`
	VaultsSynced []string          `json:"vaults_synced"`
	FilesCount   int               `json:"files_count"`
	Errors       []string          `json:"errors,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func handleRequest(ctx context.Context, event Event) (Response, error) {
	
	// Convert event types
	handlerEvent := handler.Event{
		Action:     event.Action,
		VaultID:    event.VaultID,
		SyncType:   event.SyncType,
		DestBucket: event.DestBucket,
		DestPrefix: event.DestPrefix,
	}
	
	handlerResponse, err := h.ProcessEvent(ctx, handlerEvent)
	if err != nil {
		return Response{
			Success: false,
			Message: "Handler failed",
			Errors:  []string{err.Error()},
		}, nil
	}
	
	// Convert response types
	return Response{
		Success:      handlerResponse.Success,
		Message:      handlerResponse.Message,
		VaultsSynced: handlerResponse.VaultsSynced,
		FilesCount:   handlerResponse.FilesCount,
		Errors:       handlerResponse.Errors,
		Metadata:     handlerResponse.Metadata,
	}, nil
}

func main() {
	lambda.Start(handleRequest)
}