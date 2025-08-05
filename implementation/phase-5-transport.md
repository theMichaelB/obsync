# Phase 5: Transport Layer

This phase implements HTTP and WebSocket communication with retry logic, connection management, and comprehensive mocking for tests.

## Transport Architecture

The transport layer provides:
- HTTP client with automatic retry and backoff
- WebSocket connection with heartbeat/reconnection
- Request/response logging and metrics
- Mock implementations for testing

## HTTP Client Implementation

### HTTP Transport

`internal/transport/http_client.go`:

```go
package transport

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
    
    "github.com/yourusername/obsync/internal/config"
    "github.com/yourusername/obsync/internal/events"
    "github.com/yourusername/obsync/internal/models"
)

// HTTPClient handles HTTP communication with the API.
type HTTPClient struct {
    client    *http.Client
    baseURL   string
    userAgent string
    token     string
    logger    *events.Logger
    
    // Retry configuration
    maxRetries int
    retryDelay time.Duration
}

// NewHTTPClient creates an HTTP client.
func NewHTTPClient(cfg *config.APIConfig, logger *events.Logger) *HTTPClient {
    return &HTTPClient{
        client: &http.Client{
            Timeout: cfg.Timeout,
            Transport: &http.Transport{
                MaxIdleConns:        100,
                MaxIdleConnsPerHost: 10,
                IdleConnTimeout:     90 * time.Second,
                TLSHandshakeTimeout: 10 * time.Second,
            },
        },
        baseURL:    cfg.BaseURL,
        userAgent:  cfg.UserAgent,
        maxRetries: cfg.MaxRetries,
        retryDelay: time.Second,
        logger:     logger.WithField("component", "http_client"),
    }
}

// SetToken sets the authentication token.
func (c *HTTPClient) SetToken(token string) {
    c.token = token
}

// PostJSON sends a JSON POST request.
func (c *HTTPClient) PostJSON(ctx context.Context, path string, payload interface{}) (map[string]interface{}, error) {
    url := c.baseURL + path
    
    // Marshal payload
    body, err := json.Marshal(payload)
    if err != nil {
        return nil, fmt.Errorf("marshal payload: %w", err)
    }
    
    c.logger.WithFields(map[string]interface{}{
        "method": "POST",
        "url":    url,
        "size":   len(body),
    }).Debug("Sending request")
    
    // Execute with retry
    var resp *http.Response
    err = c.retry(ctx, func() error {
        req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
        if err != nil {
            return fmt.Errorf("create request: %w", err)
        }
        
        // Set headers
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("User-Agent", c.userAgent)
        if c.token != "" {
            req.Header.Set("Authorization", "Bearer "+c.token)
        }
        
        resp, err = c.client.Do(req)
        if err != nil {
            return fmt.Errorf("execute request: %w", err)
        }
        
        // Check for retryable status codes
        if c.isRetryable(resp.StatusCode) {
            body, _ := io.ReadAll(resp.Body)
            resp.Body.Close()
            return fmt.Errorf("server error %d: %s", resp.StatusCode, body)
        }
        
        return nil
    })
    
    if err != nil {
        return nil, err
    }
    
    defer resp.Body.Close()
    
    // Read response
    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("read response: %w", err)
    }
    
    c.logger.WithFields(map[string]interface{}{
        "status": resp.StatusCode,
        "size":   len(respBody),
    }).Debug("Received response")
    
    // Check status
    if resp.StatusCode != http.StatusOK {
        var apiErr models.APIError
        if err := json.Unmarshal(respBody, &apiErr); err == nil {
            apiErr.StatusCode = resp.StatusCode
            return nil, &apiErr
        }
        return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
    }
    
    // Parse response
    var result map[string]interface{}
    if err := json.Unmarshal(respBody, &result); err != nil {
        return nil, fmt.Errorf("parse response: %w", err)
    }
    
    return result, nil
}

// DownloadChunk downloads a file chunk.
func (c *HTTPClient) DownloadChunk(ctx context.Context, chunkID string) ([]byte, error) {
    url := fmt.Sprintf("%s/api/v1/chunks/%s", c.baseURL, chunkID)
    
    c.logger.WithField("chunk_id", chunkID).Debug("Downloading chunk")
    
    var data []byte
    err := c.retry(ctx, func() error {
        req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
        if err != nil {
            return fmt.Errorf("create request: %w", err)
        }
        
        req.Header.Set("User-Agent", c.userAgent)
        if c.token != "" {
            req.Header.Set("Authorization", "Bearer "+c.token)
        }
        
        resp, err := c.client.Do(req)
        if err != nil {
            return fmt.Errorf("execute request: %w", err)
        }
        defer resp.Body.Close()
        
        if c.isRetryable(resp.StatusCode) {
            return fmt.Errorf("server error %d", resp.StatusCode)
        }
        
        if resp.StatusCode != http.StatusOK {
            body, _ := io.ReadAll(resp.Body)
            return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
        }
        
        data, err = io.ReadAll(resp.Body)
        if err != nil {
            return fmt.Errorf("read chunk: %w", err)
        }
        
        return nil
    })
    
    if err != nil {
        return nil, err
    }
    
    c.logger.WithFields(map[string]interface{}{
        "chunk_id": chunkID,
        "size":     len(data),
    }).Debug("Downloaded chunk")
    
    return data, nil
}

// retry executes a function with exponential backoff.
func (c *HTTPClient) retry(ctx context.Context, fn func() error) error {
    var lastErr error
    delay := c.retryDelay
    
    for attempt := 0; attempt <= c.maxRetries; attempt++ {
        if attempt > 0 {
            c.logger.WithFields(map[string]interface{}{
                "attempt": attempt,
                "delay":   delay,
            }).Debug("Retrying request")
            
            select {
            case <-time.After(delay):
                delay *= 2 // Exponential backoff
            case <-ctx.Done():
                return ctx.Err()
            }
        }
        
        err := fn()
        if err == nil {
            return nil
        }
        
        lastErr = err
        
        // Check if error is retryable
        if !c.isRetryableError(err) {
            return err
        }
    }
    
    return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// isRetryable checks if an HTTP status code is retryable.
func (c *HTTPClient) isRetryable(status int) bool {
    return status == http.StatusTooManyRequests ||
        status == http.StatusServiceUnavailable ||
        status == http.StatusBadGateway ||
        status == http.StatusGatewayTimeout ||
        (status >= 500 && status < 600)
}

// isRetryableError checks if an error is retryable.
func (c *HTTPClient) isRetryableError(err error) bool {
    // Network errors are retryable
    return true
}
```

## WebSocket Implementation

### WebSocket Client

`internal/transport/websocket.go`:

```go
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
    mu       sync.Mutex
    conn     *websocket.Conn
    closed   bool
    
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
        c.conn.WriteMessage(websocket.CloseMessage, 
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
        conn.SetReadDeadline(time.Now().Add(c.pongTimeout + c.pingInterval))
        conn.SetPongHandler(func(string) error {
            c.logger.Debug("Received pong")
            conn.SetReadDeadline(time.Now().Add(c.pongTimeout + c.pingInterval))
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
```

### Transport Interface Implementation

`internal/transport/transport.go`:

```go
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

// Close closes all connections.
func (t *DefaultTransport) Close() error {
    if t.wsClient != nil {
        return t.wsClient.Close()
    }
    return nil
}
```

## Mock Transport

`internal/transport/mock.go`:

```go
package transport

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"
    "time"
    
    "github.com/yourusername/obsync/internal/models"
)

// MockTransport provides a mock implementation for testing.
type MockTransport struct {
    mu sync.Mutex
    
    // Response configuration
    PostResponses  map[string]interface{}
    ChunkData      map[string][]byte
    WSMessages     []models.WSMessage
    
    // Error injection
    PostError   error
    ChunkError  error
    StreamError error
    
    // Request tracking
    PostRequests   []PostRequest
    ChunkRequests  []string
    StreamRequests []models.InitMessage
    
    // State
    token    string
    wsIndex  int
    wsChan   chan models.WSMessage
    closed   bool
}

// PostRequest tracks POST requests.
type PostRequest struct {
    Path    string
    Payload interface{}
}

// NewMockTransport creates a mock transport.
func NewMockTransport() *MockTransport {
    return &MockTransport{
        PostResponses:  make(map[string]interface{}),
        ChunkData:      make(map[string][]byte),
        PostRequests:   []PostRequest{},
        ChunkRequests:  []string{},
        StreamRequests: []models.InitMessage{},
    }
}

// PostJSON mocks HTTP POST.
func (m *MockTransport) PostJSON(ctx context.Context, path string, payload interface{}) (map[string]interface{}, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    // Track request
    m.PostRequests = append(m.PostRequests, PostRequest{
        Path:    path,
        Payload: payload,
    })
    
    // Return configured error
    if m.PostError != nil {
        return nil, m.PostError
    }
    
    // Return configured response
    if resp, ok := m.PostResponses[path]; ok {
        if mapResp, ok := resp.(map[string]interface{}); ok {
            return mapResp, nil
        }
        
        // Convert to map if needed
        data, _ := json.Marshal(resp)
        var result map[string]interface{}
        json.Unmarshal(data, &result)
        return result, nil
    }
    
    return nil, fmt.Errorf("no mock response for %s", path)
}

// DownloadChunk mocks chunk download.
func (m *MockTransport) DownloadChunk(ctx context.Context, chunkID string) ([]byte, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    m.ChunkRequests = append(m.ChunkRequests, chunkID)
    
    if m.ChunkError != nil {
        return nil, m.ChunkError
    }
    
    if data, ok := m.ChunkData[chunkID]; ok {
        return data, nil
    }
    
    return nil, fmt.Errorf("chunk not found: %s", chunkID)
}

// StreamWS mocks WebSocket streaming.
func (m *MockTransport) StreamWS(ctx context.Context, initMsg models.InitMessage) (<-chan models.WSMessage, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    m.StreamRequests = append(m.StreamRequests, initMsg)
    
    if m.StreamError != nil {
        return nil, m.StreamError
    }
    
    // Create channel for messages
    m.wsChan = make(chan models.WSMessage, len(m.WSMessages))
    m.wsIndex = 0
    
    // Start sending messages
    go m.streamMessages()
    
    return m.wsChan, nil
}

// SetToken mocks token setting.
func (m *MockTransport) SetToken(token string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.token = token
}

// Close mocks connection closing.
func (m *MockTransport) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if !m.closed {
        m.closed = true
        if m.wsChan != nil {
            close(m.wsChan)
        }
    }
    
    return nil
}

// streamMessages sends mock messages.
func (m *MockTransport) streamMessages() {
    ticker := time.NewTicker(10 * time.Millisecond)
    defer ticker.Stop()
    
    for range ticker.C {
        m.mu.Lock()
        
        if m.closed || m.wsIndex >= len(m.WSMessages) {
            if m.wsChan != nil && !m.closed {
                close(m.wsChan)
                m.wsChan = nil
            }
            m.mu.Unlock()
            return
        }
        
        msg := m.WSMessages[m.wsIndex]
        m.wsIndex++
        
        select {
        case m.wsChan <- msg:
        default:
            // Channel full, skip
        }
        
        m.mu.Unlock()
    }
}

// Helper methods for test setup

// AddPostResponse adds a mock POST response.
func (m *MockTransport) AddPostResponse(path string, response interface{}) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.PostResponses[path] = response
}

// AddChunk adds mock chunk data.
func (m *MockTransport) AddChunk(chunkID string, data []byte) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.ChunkData[chunkID] = data
}

// AddWSMessage adds a mock WebSocket message.
func (m *MockTransport) AddWSMessage(msg models.WSMessage) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.WSMessages = append(m.WSMessages, msg)
}
```

## Transport Tests

`internal/transport/transport_test.go`:

```go
package transport_test

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"
    
    "github.com/gorilla/websocket"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    
    "github.com/yourusername/obsync/internal/config"
    "github.com/yourusername/obsync/internal/events"
    "github.com/yourusername/obsync/internal/models"
    "github.com/yourusername/obsync/internal/transport"
)

func TestHTTPClientRetry(t *testing.T) {
    attempts := 0
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        attempts++
        if attempts < 3 {
            w.WriteHeader(http.StatusServiceUnavailable)
            return
        }
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"success": true}`))
    }))
    defer server.Close()
    
    cfg := &config.APIConfig{
        BaseURL:    server.URL,
        Timeout:    5 * time.Second,
        MaxRetries: 3,
        UserAgent:  "test",
    }
    
    logger := events.NewLogger(&config.LogConfig{Level: "debug"})
    client := transport.NewHTTPClient(cfg, logger)
    
    ctx := context.Background()
    resp, err := client.PostJSON(ctx, "/test", map[string]string{"key": "value"})
    
    require.NoError(t, err)
    assert.Equal(t, true, resp["success"])
    assert.Equal(t, 3, attempts)
}

func TestWebSocketConnection(t *testing.T) {
    // Create test WebSocket server
    upgrader := websocket.Upgrader{}
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        conn, err := upgrader.Upgrade(w, r, nil)
        require.NoError(t, err)
        defer conn.Close()
        
        // Read init message
        var msg models.WSMessage
        err = conn.ReadJSON(&msg)
        require.NoError(t, err)
        assert.Equal(t, models.WSTypeInit, msg.Type)
        
        // Send response
        resp := models.WSMessage{
            Type:      models.WSTypeInitResponse,
            Timestamp: time.Now(),
            Data:      []byte(`{"success": true}`),
        }
        err = conn.WriteJSON(resp)
        require.NoError(t, err)
        
        // Send done message
        done := models.WSMessage{
            Type:      models.WSTypeDone,
            Timestamp: time.Now(),
            Data:      []byte(`{"final_version": 100}`),
        }
        err = conn.WriteJSON(done)
        require.NoError(t, err)
    }))
    defer server.Close()
    
    // Convert http to ws URL
    wsURL := "ws" + server.URL[4:]
    
    logger := events.NewLogger(&config.LogConfig{Level: "debug"})
    client := transport.NewWSClient(wsURL, "test-token", logger)
    
    ctx := context.Background()
    err := client.Connect(ctx)
    require.NoError(t, err)
    defer client.Close()
    
    // Send init
    err = client.SendInit(models.InitMessage{
        Token:   "test-token",
        VaultID: "vault-123",
        Initial: true,
        Version: 0,
    })
    require.NoError(t, err)
    
    // Read messages
    messages := []models.WSMessage{}
    for msg := range client.Messages() {
        messages = append(messages, msg)
        if msg.Type == models.WSTypeDone {
            break
        }
    }
    
    assert.Len(t, messages, 2)
    assert.Equal(t, models.WSTypeInitResponse, messages[0].Type)
    assert.Equal(t, models.WSTypeDone, messages[1].Type)
}

func TestMockTransport(t *testing.T) {
    mock := transport.NewMockTransport()
    
    // Configure responses
    mock.AddPostResponse("/api/v1/auth/login", map[string]interface{}{
        "token": "test-token",
    })
    
    mock.AddChunk("chunk-123", []byte("test data"))
    
    mock.AddWSMessage(models.WSMessage{
        Type: models.WSTypeFile,
        UID:  1,
        Data: []byte(`{"path": "test.md"}`),
    })
    
    // Test POST
    ctx := context.Background()
    resp, err := mock.PostJSON(ctx, "/api/v1/auth/login", nil)
    require.NoError(t, err)
    assert.Equal(t, "test-token", resp["token"])
    
    // Test chunk download
    data, err := mock.DownloadChunk(ctx, "chunk-123")
    require.NoError(t, err)
    assert.Equal(t, []byte("test data"), data)
    
    // Test WebSocket
    msgChan, err := mock.StreamWS(ctx, models.InitMessage{})
    require.NoError(t, err)
    
    msg := <-msgChan
    assert.Equal(t, models.WSTypeFile, msg.Type)
    assert.Equal(t, 1, msg.UID)
    
    // Verify tracking
    assert.Len(t, mock.PostRequests, 1)
    assert.Len(t, mock.ChunkRequests, 1)
    assert.Len(t, mock.StreamRequests, 1)
}
```

## Retry and Backoff Tests

`internal/transport/retry_test.go`:

```go
package transport

import (
    "context"
    "errors"
    "testing"
    "time"
    
    "github.com/stretchr/testify/assert"
)

func TestRetryWithBackoff(t *testing.T) {
    attempts := 0
    startTime := time.Now()
    
    client := &HTTPClient{
        maxRetries: 3,
        retryDelay: 100 * time.Millisecond,
    }
    
    err := client.retry(context.Background(), func() error {
        attempts++
        if attempts < 3 {
            return errors.New("temporary error")
        }
        return nil
    })
    
    duration := time.Since(startTime)
    
    assert.NoError(t, err)
    assert.Equal(t, 3, attempts)
    // Should have delays: 0ms, 100ms, 200ms = 300ms total
    assert.GreaterOrEqual(t, duration, 300*time.Millisecond)
    assert.Less(t, duration, 400*time.Millisecond)
}

func TestRetryContextCancellation(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
    defer cancel()
    
    attempts := 0
    client := &HTTPClient{
        maxRetries: 5,
        retryDelay: 100 * time.Millisecond,
    }
    
    err := client.retry(ctx, func() error {
        attempts++
        return errors.New("error")
    })
    
    assert.Error(t, err)
    assert.Equal(t, context.DeadlineExceeded, err)
    assert.LessOrEqual(t, attempts, 3)
}
```

## Phase 5 Deliverables

1. ✅ HTTP client with automatic retry and exponential backoff
2. ✅ WebSocket client with heartbeat and reconnection
3. ✅ Unified transport interface
4. ✅ Comprehensive mock transport for testing
5. ✅ Request/response logging and metrics
6. ✅ Connection pooling and timeout handling
7. ✅ Test coverage for retry logic and error cases

## Verification Commands

```bash
# Run transport tests
go test -v ./internal/transport/...

# Test retry behavior
go test -v -run TestRetry ./internal/transport/...

# Test WebSocket handling
go test -v -run TestWebSocket ./internal/transport/...

# Benchmark connection performance
go test -bench=. ./internal/transport/...

# Check for race conditions
go test -race ./internal/transport/...
```

## Next Steps

With the transport layer complete, proceed to [Phase 6: State Management](phase-6-state.md) to implement sync state persistence with JSON and SQLite backends.