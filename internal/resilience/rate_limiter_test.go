package resilience

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestDoWithRateLimit_429ThenSuccess verifies that a 429 with Retry-After: 1 does
// not count against MaxAttempts and that the final 200 is returned successfully.
func TestDoWithRateLimit_429ThenSuccess(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	retryCfg := RetryConfig{MaxAttempts: 2, BaseDelayMs: 1, MaxDelayMs: 10}
	start := time.Now()

	resp, err := DoWithRateLimit(context.Background(), retryCfg, 10, 5, func(ctx context.Context) (*http.Response, error) {
		return http.Get(srv.URL) //nolint:noctx
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Verify 429 did NOT count as a retry attempt (both calls used up: 1 for 429, 1 for 200).
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls, got %d", calls.Load())
	}
	// Verify we slept at least ~1 second for Retry-After.
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected at least 1s wait, got %v", elapsed)
	}
}

// TestDoWithRateLimit_429NoHeaderFallback verifies that when Retry-After is absent,
// the fallback delay is used (we use a very short fallback for the test).
func TestDoWithRateLimit_429NoHeaderFallback(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			// No Retry-After header.
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	retryCfg := RetryConfig{MaxAttempts: 3, BaseDelayMs: 1, MaxDelayMs: 10}
	// fallbackDelaySecs = 1, maxWaitSecs = 10.
	start := time.Now()

	resp, err := DoWithRateLimit(context.Background(), retryCfg, 10, 1, func(ctx context.Context) (*http.Response, error) {
		return http.Get(srv.URL) //nolint:noctx
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Should have slept ~1s (fallback).
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected at least 1s fallback wait, got %v", elapsed)
	}
}

// TestDoWithRateLimit_429ExceedsMaxWait verifies that when Retry-After exceeds
// maxWaitSecs, a RateLimitExceededError is returned.
func TestDoWithRateLimit_429ExceedsMaxWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return 429 with a very long Retry-After.
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	retryCfg := RetryConfig{MaxAttempts: 3, BaseDelayMs: 1, MaxDelayMs: 10}
	// maxWaitSecs = 60, Retry-After = 3600 > 60.
	_, err := DoWithRateLimit(context.Background(), retryCfg, 60, 5, func(ctx context.Context) (*http.Response, error) {
		return http.Get(srv.URL) //nolint:noctx
	})

	if err == nil {
		t.Fatal("expected RateLimitExceededError, got nil")
	}
	rlErr, ok := err.(*RateLimitExceededError)
	if !ok {
		t.Fatalf("expected *RateLimitExceededError, got %T: %v", err, err)
	}
	if rlErr.RetryAfterSecs != 3600 {
		t.Errorf("expected RetryAfterSecs=3600, got %d", rlErr.RetryAfterSecs)
	}
}
