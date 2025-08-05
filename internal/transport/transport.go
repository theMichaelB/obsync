package transport

import (
	"context"
	"fmt"
	"time"

	"github.com/TheMichaelB/obsync/internal/config"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/models"
)

// Transport combines HTTP and WebSocket functionality.
type Transport interface {
	// HTTP methods
	PostJSON(ctx context.Context, path string, payload interface{}) (map[string]interface{}, error)
	DownloadChunk(ctx context.Context, chunkID string) ([]byte, error)

	// WebSocket methods
	StreamWS(ctx context.Context, host string, initMsg models.InitMessage) (<-chan models.WSMessage, error)
	SendMessage(msg interface{}) error
	ReceiveBinaryMessage(ctx context.Context, timeout time.Duration) ([]byte, error)
	ReceiveJSONMessage(ctx context.Context, timeout time.Duration) (map[string]interface{}, error)

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
func (t *DefaultTransport) StreamWS(ctx context.Context, host string, initMsg models.InitMessage) (<-chan models.WSMessage, error) {
	// Create new WS client for this stream using vault host
	wsURL := "wss://" + host + "/"
	t.wsClient = NewWSClient(wsURL, t.httpClient.token, t.logger)

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

// SendMessage sends a message via WebSocket.
func (t *DefaultTransport) SendMessage(msg interface{}) error {
	if t.wsClient == nil {
		return fmt.Errorf("websocket not connected")
	}
	return t.wsClient.SendMessage(msg)
}

// ReceiveBinaryMessage receives a binary message via WebSocket.
func (t *DefaultTransport) ReceiveBinaryMessage(ctx context.Context, timeout time.Duration) ([]byte, error) {
	if t.wsClient == nil {
		return nil, fmt.Errorf("websocket not connected")
	}
	return t.wsClient.ReceiveBinaryMessage(ctx, timeout)
}

// ReceiveJSONMessage receives a JSON message via WebSocket.
func (t *DefaultTransport) ReceiveJSONMessage(ctx context.Context, timeout time.Duration) (map[string]interface{}, error) {
	if t.wsClient == nil {
		return nil, fmt.Errorf("websocket not connected")
	}
	return t.wsClient.ReceiveJSONMessage(ctx, timeout)
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