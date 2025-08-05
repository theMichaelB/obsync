package transport

import (
	"context"
	"fmt"

	"github.com/yourusername/obsync/internal/config"
	"github.com/yourusername/obsync/internal/events"
	"github.com/yourusername/obsync/internal/models"
)

// Transport combines HTTP and WebSocket functionality.
type Transport interface {
	// HTTP methods
	PostJSON(ctx context.Context, path string, payload interface{}) (map[string]interface{}, error)
	DownloadChunk(ctx context.Context, chunkID string) ([]byte, error)

	// WebSocket methods
	StreamWS(ctx context.Context, initMsg models.InitMessage) (<-chan models.WSMessage, error)

	// Authentication
	SetToken(token string)
	GetToken() string

	// Lifecycle
	Close() error
}

// DefaultTransport implements the Transport interface.
type DefaultTransport struct {
	httpClient *HTTPClient
	wsClient   *WSClient
	logger     *events.Logger
}

// NewTransport creates a transport instance.
func NewTransport(cfg *config.APIConfig, logger *events.Logger) Transport {
	return &DefaultTransport{
		httpClient: NewHTTPClient(cfg, logger),
		logger:     logger,
	}
}

// PostJSON forwards to HTTP client.
func (t *DefaultTransport) PostJSON(ctx context.Context, path string, payload interface{}) (map[string]interface{}, error) {
	return t.httpClient.PostJSON(ctx, path, payload)
}

// DownloadChunk forwards to HTTP client.
func (t *DefaultTransport) DownloadChunk(ctx context.Context, chunkID string) ([]byte, error) {
	return t.httpClient.DownloadChunk(ctx, chunkID)
}

// StreamWS creates a WebSocket stream.
func (t *DefaultTransport) StreamWS(ctx context.Context, initMsg models.InitMessage) (<-chan models.WSMessage, error) {
	// Create new WS client for this stream
	t.wsClient = NewWSClient(t.httpClient.baseURL, t.httpClient.token, t.logger)

	// Connect
	if err := t.wsClient.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect websocket: %w", err)
	}

	// Send init
	if err := t.wsClient.SendInit(initMsg); err != nil {
		t.wsClient.Close()
		return nil, fmt.Errorf("send init: %w", err)
	}

	// Monitor errors in background
	go func() {
		for err := range t.wsClient.Errors() {
			t.logger.WithError(err).Error("WebSocket error")
		}
	}()

	return t.wsClient.Messages(), nil
}

// SetToken sets the auth token.
func (t *DefaultTransport) SetToken(token string) {
	t.httpClient.SetToken(token)
}

// GetToken returns the current auth token.
func (t *DefaultTransport) GetToken() string {
	return t.httpClient.GetToken()
}

// Close closes all connections.
func (t *DefaultTransport) Close() error {
	if t.wsClient != nil {
		return t.wsClient.Close()
	}
	return nil
}