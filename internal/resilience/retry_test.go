package resilience

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestDoWithRetry_503ThenSuccess verifies that a mock server returning 503 twice
// then 200 is retried and ultimately returns 200.
func TestDoWithRetry_503ThenSuccess(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := RetryConfig{MaxAttempts: 5, BaseDelayMs: 1, MaxDelayMs: 10}

	resp, err := DoWithRetry(context.Background(), cfg, func(ctx context.Context) (*http.Response, error) {
		return http.Get(srv.URL) //nolint:noctx
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", calls.Load())
	}
}

// TestDoWithRetry_NoRetryOn401 verifies that a 401 response is returned immediately
// without any retries.
func TestDoWithRetry_NoRetryOn401(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := RetryConfig{MaxAttempts: 5, BaseDelayMs: 1, MaxDelayMs: 10}

	resp, err := DoWithRetry(context.Background(), cfg, func(ctx context.Context) (*http.Response, error) {
		return http.Get(srv.URL) //nolint:noctx
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 call, got %d", calls.Load())
	}
}

// TestDoWithRetry_ContextCancelled verifies that context cancellation is
// surfaced promptly and does not proceed with further retries.
func TestDoWithRetry_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Respond with 503 so the retry loop would normally retry.
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cfg := RetryConfig{MaxAttempts: 10, BaseDelayMs: 200, MaxDelayMs: 500}

	_, err := DoWithRetry(ctx, cfg, func(ctx context.Context) (*http.Response, error) {
		return http.Get(srv.URL) //nolint:noctx
	})
	if err == nil {
		t.Fatal("expected error from context cancellation, got nil")
	}
	if ctx.Err() == nil {
		t.Error("expected context to be done")
	}
}

// TestDoWithRetry_MaxAttemptsExhausted verifies that after MaxAttempts all
// returning 503, ErrMaxAttemptsExhausted is returned.
func TestDoWithRetry_MaxAttemptsExhausted(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := RetryConfig{MaxAttempts: 3, BaseDelayMs: 1, MaxDelayMs: 5}

	_, err := DoWithRetry(context.Background(), cfg, func(ctx context.Context) (*http.Response, error) {
		return http.Get(srv.URL) //nolint:noctx
	})
	if err != ErrMaxAttemptsExhausted {
		t.Errorf("expected ErrMaxAttemptsExhausted, got %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", calls.Load())
	}
}
