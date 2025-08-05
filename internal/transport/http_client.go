package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/net/http2"

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
	// Create transport with HTTP/2 support
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
		},
	}

	// Configure HTTP/2
	if err := http2.ConfigureTransport(transport); err != nil {
		logger.WithError(err).Warn("Failed to configure HTTP/2")
	}

	return &HTTPClient{
		client: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
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

// GetToken returns the current authentication token.
func (c *HTTPClient) GetToken() string {
	return c.token
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
		"body":   string(body),
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
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Origin", "app://obsidian.md")
		req.Header.Set("User-Agent", c.userAgent)
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		// Debug: log all headers being sent
		c.logger.WithFields(map[string]interface{}{
			"headers": req.Header,
		}).Debug("Request headers")

		resp, err = c.client.Do(req)
		if err != nil {
			return fmt.Errorf("execute request: %w", err)
		}

		// Check for retryable status codes
		if c.isRetryable(resp.StatusCode) {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("server error %d: %s", resp.StatusCode, respBody)
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
		"body":   string(respBody),
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