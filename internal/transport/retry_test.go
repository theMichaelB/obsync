package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/TheMichaelB/obsync/internal/events"
)

func TestRetryWithBackoff(t *testing.T) {
	attempts := 0
	startTime := time.Now()

	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := &HTTPClient{
		maxRetries: 3,
		retryDelay: 100 * time.Millisecond,
		logger:     logger,
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
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := &HTTPClient{
		maxRetries: 5,
		retryDelay: 100 * time.Millisecond,
		logger:     logger,
	}

	err := client.retry(ctx, func() error {
		attempts++
		return errors.New("error")
	})

	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.LessOrEqual(t, attempts, 3)
}

func TestRetryMaxAttemptsExceeded(t *testing.T) {
	attempts := 0
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := &HTTPClient{
		maxRetries: 2,
		retryDelay: 10 * time.Millisecond,
		logger:     logger,
	}

	err := client.retry(context.Background(), func() error {
		attempts++
		return errors.New("persistent error")
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max retries exceeded")
	assert.Equal(t, 3, attempts) // maxRetries + 1
}

func TestRetryNonRetryableError(t *testing.T) {
	// For this test we'll simulate a non-retryable error by using context cancellation
	// which is handled as non-retryable
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	attempts := 0
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := &HTTPClient{
		maxRetries: 3,
		retryDelay: 10 * time.Millisecond,
		logger:     logger,
	}

	err := client.retry(ctx, func() error {
		attempts++
		return errors.New("some error")
	})

	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, 1, attempts) // Should not retry due to context cancellation
}

func TestRetrySuccessOnFirstAttempt(t *testing.T) {
	attempts := 0
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := &HTTPClient{
		maxRetries: 3,
		retryDelay: 100 * time.Millisecond,
		logger:     logger,
	}

	err := client.retry(context.Background(), func() error {
		attempts++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, attempts)
}

func TestIsRetryableStatusCode(t *testing.T) {
	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := &HTTPClient{logger: logger}

	tests := []struct {
		status   int
		expected bool
	}{
		{200, false},    // OK
		{400, false},    // Bad Request
		{401, false},    // Unauthorized
		{404, false},    // Not Found
		{429, true},     // Too Many Requests
		{500, true},     // Internal Server Error
		{502, true},     // Bad Gateway
		{503, true},     // Service Unavailable
		{504, true},     // Gateway Timeout
		{599, true},     // Other 5xx
		{600, false},    // Not in 5xx range
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			result := client.isRetryable(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRetryExponentialBackoff(t *testing.T) {
	attempts := 0
	delays := []time.Duration{}
	startTime := time.Now()

	var buf bytes.Buffer
	logger := events.NewTestLogger(events.DebugLevel, "json", &buf)
	client := &HTTPClient{
		maxRetries: 4,
		retryDelay: 50 * time.Millisecond,
		logger:     logger,
	}

	err := client.retry(context.Background(), func() error {
		if attempts > 0 {
			delays = append(delays, time.Since(startTime))
		}
		startTime = time.Now()
		attempts++
		if attempts < 4 {
			return errors.New("error")
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 4, attempts)
	assert.Len(t, delays, 3)

	// Verify exponential backoff: 50ms, 100ms, 200ms
	assert.GreaterOrEqual(t, delays[0], 50*time.Millisecond)
	assert.Less(t, delays[0], 80*time.Millisecond)

	assert.GreaterOrEqual(t, delays[1], 100*time.Millisecond)
	assert.Less(t, delays[1], 130*time.Millisecond)

	assert.GreaterOrEqual(t, delays[2], 200*time.Millisecond)
	assert.Less(t, delays[2], 230*time.Millisecond)
}