package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/models"
)

// WSClient handles WebSocket communication.
type WSClient struct {
	url       string
	token     string
	logger    *events.Logger

	// Connection state
	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool

	// Channels
	messages chan models.WSMessage
	errors   chan error
	done     chan struct{}

	// Heartbeat
	pingInterval time.Duration
	pongTimeout  time.Duration
}

// NewWSClient creates a WebSocket client.
func NewWSClient(wsURL, token string, logger *events.Logger) *WSClient {
	// If it's not already a WebSocket URL, convert http(s) to ws(s)
	if len(wsURL) > 4 && wsURL[:4] == "http" {
		wsURL = "ws" + wsURL[4:] // Convert http(s) to ws(s)
	}

	return &WSClient{
		url:          wsURL,
		token:        token,
		logger:       logger.WithField("component", "ws_client"),
		messages:     make(chan models.WSMessage, 100),
		errors:       make(chan error, 10),
		done:         make(chan struct{}),
		pingInterval: 30 * time.Second,
		pongTimeout:  10 * time.Second,
	}
}

// Connect establishes WebSocket connection.
func (c *WSClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return fmt.Errorf("already connected")
	}

	c.logger.WithField("url", c.url).Info("Connecting to WebSocket")

	// Set up headers
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.token)

	// Connect with timeout
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.DialContext(ctx, c.url, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("websocket connect failed (HTTP %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("websocket connect failed: %w", err)
	}

	c.conn = conn
	c.closed = false

	// Start goroutines
	go c.readLoop()
	go c.pingLoop()

	c.logger.Info("WebSocket connected")
	return nil
}

// SendInit sends the initialization message.
func (c *WSClient) SendInit(msg models.InitMessage) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	c.logger.WithFields(map[string]interface{}{
		"vault_id": msg.ID,
		"initial":  msg.Initial,
		"version":  msg.Version,
		"keyhash":  func() string {
			if len(msg.Keyhash) >= 8 {
				return msg.Keyhash[:8] + "..."
			}
			return msg.Keyhash
		}(), // Log first 8 chars for debugging
		"full_message": msg,
	}).Debug("Sending init message")

	// Send init message directly as flat JSON (Obsidian protocol)
	if err := conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("send init: %w", err)
	}

	return nil
}

// SendMessage sends a generic WebSocket message.
func (c *WSClient) SendMessage(msg interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	if err := c.conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

// ReceiveBinaryMessage waits for a binary WebSocket message with timeout.
func (c *WSClient) ReceiveBinaryMessage(ctx context.Context, timeout time.Duration) ([]byte, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	c.logger.WithField("timeout", timeout).Debug("Waiting for binary message")

	// Set read deadline
	deadline := time.Now().Add(timeout)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	// Read binary message
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read binary message: %w", err)
	}

	if messageType != websocket.BinaryMessage {
		return nil, fmt.Errorf("expected binary message, got message type %d", messageType)
	}

	c.logger.WithField("size", len(data)).Debug("Received binary message")
	return data, nil
}

// ReceiveJSONMessage waits for a JSON WebSocket message with timeout.
func (c *WSClient) ReceiveJSONMessage(ctx context.Context, timeout time.Duration) (map[string]interface{}, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	c.logger.WithField("timeout", timeout).Debug("Waiting for JSON message")

	// Set read deadline
	deadline := time.Now().Add(timeout)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	// Read text message
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read JSON message: %w", err)
	}

	if messageType != websocket.TextMessage {
		return nil, fmt.Errorf("expected text message, got message type %d", messageType)
	}

	// Parse JSON
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse JSON message: %w", err)
	}

	c.logger.WithFields(map[string]interface{}{
		"size": len(data),
		"keys": len(result),
	}).Debug("Received JSON message")
	
	return result, nil
}

// Messages returns the message channel.
func (c *WSClient) Messages() <-chan models.WSMessage {
	return c.messages
}

// Errors returns the error channel.
func (c *WSClient) Errors() <-chan error {
	return c.errors
}

// Close closes the WebSocket connection.
func (c *WSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	close(c.done)

	if c.conn != nil {
		// Send close message
		_ = c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

		err := c.conn.Close()
		c.conn = nil
		return err
	}

	return nil
}

// readLoop reads messages from WebSocket.
func (c *WSClient) readLoop() {
	defer func() {
		c.Close()
		close(c.messages)
		close(c.errors)
	}()

	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()

		if conn == nil {
			return
		}

		// Set read deadline for pong
		_ = conn.SetReadDeadline(time.Now().Add(c.pongTimeout + c.pingInterval))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(c.pongTimeout + c.pingInterval))
			return nil
		})

		// Read message with type detection
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure) {
				c.logger.WithError(err).Error("WebSocket read error")
				c.errors <- err
			}
			return
		}

		// Create appropriate message based on type
		var msg models.WSMessage
		
		if messageType == websocket.BinaryMessage {
			// Binary message - likely pull response data
			msg = models.WSMessage{
				Type:       "binary",
				IsBinary:   true,
				BinaryData: data,
				Timestamp:  time.Now(),
			}
			
		} else {
			// Text message - parse as JSON
			var rawMsg map[string]interface{}
			if err := json.Unmarshal(data, &rawMsg); err != nil {
				c.logger.WithError(err).Warn("Failed to parse JSON message")
				continue
			}
			
			c.logger.WithFields(map[string]interface{}{
				"raw_message": rawMsg,
			}).Debug("Received raw WebSocket message")

			// Convert to WSMessage structure
			msgType := c.determineMessageType(rawMsg)
			msg = models.WSMessage{
				Type:      msgType,
				UID:       getInt(rawMsg, "uid"),
				Data:      json.RawMessage(data),
				Timestamp: time.Now(),
			}
		}


		select {
		case c.messages <- msg:
		case <-c.done:
			return
		}
	}
}

// determineMessageType determines the message type from raw JSON data.
func (c *WSClient) determineMessageType(rawMsg map[string]interface{}) models.WSMessageType {
	// Check for "op" field first (standard Obsidian protocol)
	if _, exists := rawMsg["op"]; exists {
		return models.WSMessageType(getString(rawMsg, "op"))
	}
	
	// Check for "type" field (test messages and internal format)
	if _, exists := rawMsg["type"]; exists {
		return models.WSMessageType(getString(rawMsg, "type"))
	}
	
	// Check for pull response characteristics
	if _, hasHash := rawMsg["hash"]; hasHash {
		if _, hasPieces := rawMsg["pieces"]; hasPieces {
			return "pull_metadata"
		}
	}
	
	// Default to empty type for unknown messages
	return ""
}

// Helper functions for parsing raw WebSocket messages
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

// pingLoop sends periodic pings.
func (c *WSClient) pingLoop() {
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				return
			}

			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.done:
			return
		}
	}
}


