package transport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TheMichaelB/obsync/internal/config"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/models"
	"github.com/TheMichaelB/obsync/internal/transport"
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
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer server.Close()

	cfg := &config.APIConfig{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 3,
		UserAgent:  "test",
	}

	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := transport.NewHTTPClient(cfg, logger)

	ctx := context.Background()
	resp, err := client.PostJSON(ctx, "/test", map[string]string{"key": "value"})

	require.NoError(t, err)
	assert.Equal(t, true, resp["success"])
	assert.Equal(t, 3, attempts)
}

func TestHTTPClientDownloadChunk(t *testing.T) {
	testData := []byte("test chunk data")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/chunks/chunk-123", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	cfg := &config.APIConfig{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 3,
		UserAgent:  "test",
	}

	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := transport.NewHTTPClient(cfg, logger)
	client.SetToken("test-token")

	ctx := context.Background()
	data, err := client.DownloadChunk(ctx, "chunk-123")

	require.NoError(t, err)
	assert.Equal(t, testData, data)
}

func TestWebSocketConnection(t *testing.T) {
	// Create test WebSocket server
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check authorization header
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		// Read init message (sent as flat JSON)
		var initMsg models.InitMessage
		err = conn.ReadJSON(&initMsg)
		require.NoError(t, err)
		assert.Equal(t, "vault-123", initMsg.ID)

		// Send response
		resp := models.WSMessage{
			Type:      models.WSTypeInitResponse,
			Timestamp: time.Now(),
			Data:      json.RawMessage(`{"success": true}`),
		}
		err = conn.WriteJSON(resp)
		require.NoError(t, err)

		// Send done message
		done := models.WSMessage{
			Type:      models.WSTypeDone,
			Timestamp: time.Now(),
			Data:      json.RawMessage(`{"final_version": 100}`),
		}
		err = conn.WriteJSON(done)
		require.NoError(t, err)
	}))
	defer server.Close()

	// Convert http to ws URL
	wsURL := "ws" + server.URL[4:]

	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := transport.NewWSClient(wsURL, "test-token", logger)

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close()

	// Send init
	err = client.SendInit(models.InitMessage{
		Token:   "test-token",
		ID:      "vault-123",
		Initial: true,
		Version: 0,
	})
	require.NoError(t, err)

	// Read messages
	messages := []models.WSMessage{}
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg, ok := <-client.Messages():
			if !ok {
				// Channel closed
				goto done
			}
			messages = append(messages, msg)
			if msg.Type == models.WSTypeDone {
				goto done
			}
		case <-timeout:
			t.Fatal("timeout waiting for messages")
		}
	}

done:
	assert.Len(t, messages, 2)
	assert.Equal(t, models.WSTypeInitResponse, messages[0].Type)
	assert.Equal(t, models.WSTypeDone, messages[1].Type)
}

func TestTransportInterface(t *testing.T) {
	// Create HTTP test server
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "test-token"}`))
			return
		}
		if r.URL.Path == "/api/v1/chunks/chunk-123" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("chunk data"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer httpServer.Close()

	cfg := &config.APIConfig{
		BaseURL:    httpServer.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 3,
		UserAgent:  "test",
	}

	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	transport := transport.NewTransport(cfg, logger)
	defer transport.Close()

	ctx := context.Background()

	// Test POST
	resp, err := transport.PostJSON(ctx, "/api/v1/auth/login", map[string]string{
		"email":    "test@example.com",
		"password": "password",
	})
	require.NoError(t, err)
	assert.Equal(t, "test-token", resp["token"])

	// Set token
	transport.SetToken("test-token")

	// Test chunk download
	data, err := transport.DownloadChunk(ctx, "chunk-123")
	require.NoError(t, err)
	assert.Equal(t, []byte("chunk data"), data)
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
		Data: json.RawMessage(`{"path": "test.md"}`),
	})

	mock.AddWSMessage(models.WSMessage{
		Type: models.WSTypeDone,
		UID:  2,
		Data: json.RawMessage(`{"final_version": 100}`),
	})

	// Test POST
	ctx := context.Background()
	resp, err := mock.PostJSON(ctx, "/api/v1/auth/login", map[string]string{
		"email":    "test@example.com",
		"password": "password",
	})
	require.NoError(t, err)
	assert.Equal(t, "test-token", resp["token"])

	// Test chunk download
	data, err := mock.DownloadChunk(ctx, "chunk-123")
	require.NoError(t, err)
	assert.Equal(t, []byte("test data"), data)

	// Test WebSocket
	msgChan, err := mock.StreamWS(ctx, "wss://test.example.com", models.InitMessage{
		ID:      "vault-123",
		Initial: true,
	})
	require.NoError(t, err)

	// Read messages
	messages := []models.WSMessage{}
	timeout := time.After(1 * time.Second)
	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				goto done
			}
			messages = append(messages, msg)
			if msg.Type == models.WSTypeDone {
				goto done
			}
		case <-timeout:
			goto done
		}
	}

done:
	assert.Len(t, messages, 2)
	assert.Equal(t, models.WSTypeFile, messages[0].Type)
	assert.Equal(t, 1, messages[0].UID)
	assert.Equal(t, models.WSTypeDone, messages[1].Type)

	// Verify tracking
	assert.Len(t, mock.PostRequests, 1)
	assert.Equal(t, "/api/v1/auth/login", mock.PostRequests[0].Path)
	assert.Len(t, mock.ChunkRequests, 1)
	assert.Equal(t, "chunk-123", mock.ChunkRequests[0])
	assert.Len(t, mock.StreamRequests, 1)
	assert.Equal(t, "vault-123", mock.StreamRequests[0].ID)
}

func TestHTTPClientAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"error": "invalid_request",
			"message": "Missing required field",
			"code": 1001
		}`))
	}))
	defer server.Close()

	cfg := &config.APIConfig{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 3,
		UserAgent:  "test",
	}

	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := transport.NewHTTPClient(cfg, logger)

	ctx := context.Background()
	_, err := client.PostJSON(ctx, "/test", map[string]string{"key": "value"})

	require.Error(t, err)
	
	// For now, just verify it's an error since our APIError struct might need adjustment
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "invalid_request")
}