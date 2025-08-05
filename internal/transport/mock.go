package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/TheMichaelB/obsync/internal/models"
)

// MockTransport provides a mock implementation for testing.
type MockTransport struct {
	mu sync.Mutex

	// Response configuration
	PostResponses map[string]interface{}
	ChunkData     map[string][]byte
	WSMessages    []models.WSMessage

	// Error injection
	PostError   error
	ChunkError  error
	StreamError error

	// Request tracking
	PostRequests   []PostRequest
	ChunkRequests  []string
	StreamRequests []models.InitMessage

	// State
	token   string
	wsIndex int
	wsChan  chan models.WSMessage
	closed  bool
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
		_ = json.Unmarshal(data, &result)
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
	// Send all messages immediately for testing
	for {
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
		m.mu.Unlock()

		select {
		case m.wsChan <- msg:
			// Small delay to allow processing
			time.Sleep(1 * time.Millisecond)
		default:
			// Channel full, retry after delay
			time.Sleep(5 * time.Millisecond)
			continue
		}
	}
}

// Helper methods for test setup

// AddResponse adds a mock POST response (alias for AddPostResponse).
func (m *MockTransport) AddResponse(path string, response interface{}) {
	m.AddPostResponse(path, response)
}

// AddPostResponse adds a mock POST response.
func (m *MockTransport) AddPostResponse(path string, response interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PostResponses[path] = response
}

// AddError sets an error for a specific path.
func (m *MockTransport) AddError(path string, err error) {
	// For simplicity, we'll set the general PostError
	// In a more sophisticated mock, we'd track errors per path
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PostError = err
}

// GetToken returns the current token.
func (m *MockTransport) GetToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.token
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

// AddDelayedMessage adds a WebSocket message with delay (for testing cancellation).
func (m *MockTransport) AddDelayedMessage(msg models.WSMessage, delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// For simplicity, just add the message normally
	// In a real implementation, we'd handle delays
	m.WSMessages = append(m.WSMessages, msg)
}

// SendMessage mocks sending a WebSocket message.
func (m *MockTransport) SendMessage(msg interface{}) error {
	// Mock implementation - just return success
	return nil
}

// ReceiveBinaryMessage mocks receiving binary message.
func (m *MockTransport) ReceiveBinaryMessage(ctx context.Context, timeout time.Duration) ([]byte, error) {
	return []byte("mock binary data"), nil
}

// ReceiveJSONMessage mocks receiving JSON message.
func (m *MockTransport) ReceiveJSONMessage(ctx context.Context, timeout time.Duration) (map[string]interface{}, error) {
	return map[string]interface{}{
		"hash":   "mock_hash",
		"size":   100,
		"pieces": 1,
	}, nil
}