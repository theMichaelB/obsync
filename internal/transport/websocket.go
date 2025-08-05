package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yourusername/obsync/internal/events"
	"github.com/yourusername/obsync/internal/models"
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
func NewWSClient(baseURL, token string, logger *events.Logger) *WSClient {
	wsURL := baseURL
	if len(wsURL) > 4 && wsURL[:4] == "http" {
		wsURL = "ws" + wsURL[4:] // Convert http(s) to ws(s)
	}

	return &WSClient{
		url:          wsURL + "/api/v1/sync/stream",
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

	wsMsg := models.WSMessage{
		Type:      models.WSTypeInit,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal init message: %w", err)
	}
	wsMsg.Data = json.RawMessage(data)

	c.logger.WithFields(map[string]interface{}{
		"vault_id": msg.VaultID,
		"initial":  msg.Initial,
		"version":  msg.Version,
	}).Debug("Sending init message")

	if err := conn.WriteJSON(wsMsg); err != nil {
		return fmt.Errorf("send init: %w", err)
	}

	return nil
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
			c.logger.Debug("Received pong")
			_ = conn.SetReadDeadline(time.Now().Add(c.pongTimeout + c.pingInterval))
			return nil
		})

		var msg models.WSMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure) {
				c.logger.WithError(err).Error("WebSocket read error")
				c.errors <- err
			}
			return
		}

		c.logger.WithFields(map[string]interface{}{
			"type": msg.Type,
			"uid":  msg.UID,
		}).Debug("Received message")

		select {
		case c.messages <- msg:
		case <-c.done:
			return
		}
	}
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

			c.logger.Debug("Sending ping")
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.logger.WithError(err).Error("Ping failed")
				return
			}

		case <-c.done:
			return
		}
	}
}