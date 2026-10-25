// Package resilience provides retry and rate-limiting helpers for external HTTP calls.
package resilience

import (
	"context"
	"math/rand"
	"net"
	"net/http"
	"time"
)

// RetryConfig controls the behaviour of DoWithRetry.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts (including the first call).
	// A value of 1 means no retries.
	MaxAttempts int
	// BaseDelayMs is the base delay for exponential backoff, in milliseconds.
	BaseDelayMs int
	// MaxDelayMs is the upper bound on backoff delay, in milliseconds.
	MaxDelayMs int
}

// isRetryableStatusCode returns true for transient server-side errors.
func isRetryableStatusCode(code int) bool {
	switch code {
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// isRetryableNetError returns true for network errors that are worth retrying.
func isRetryableNetError(err error) bool {
	if err == nil {
		return false
	}
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout()
	}
	return false
}

// sleepWithContext sleeps for d, respecting ctx cancellation.
// Returns ctx.Err() if the context is cancelled before d elapses.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// jitterDelay computes a random delay in [0, cap] where cap = min(MaxDelayMs, BaseDelayMs * 2^attempt).
// attempt is 0-indexed (0 = after first failure).
func jitterDelay(cfg RetryConfig, attempt int) time.Duration {
	if cfg.BaseDelayMs <= 0 {
		return 0
	}
	// 2^attempt factor, capped to avoid overflow.
	factor := 1
	for i := 0; i < attempt; i++ {
		factor *= 2
		if factor*cfg.BaseDelayMs >= cfg.MaxDelayMs {
			break
		}
	}
	cap := cfg.BaseDelayMs * factor
	if cap > cfg.MaxDelayMs {
		cap = cfg.MaxDelayMs
	}
	if cap <= 0 {
		return 0
	}
	ms := rand.Intn(cap + 1)
	return time.Duration(ms) * time.Millisecond
}

// DoWithRetry calls fn up to cfg.MaxAttempts times. It retries on:
//   - network timeout errors
//   - HTTP 500, 502, 503, 504 responses
//
// It does NOT retry on 400, 401, 403, 404, 409, or any other 4xx.
// Between retries it sleeps with full-jitter exponential backoff and
// respects context cancellation.
//
// The returned *http.Response belongs to the caller; on error it is nil.
func DoWithRetry(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) (*http.Response, error)) (*http.Response, error) {
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}

	var lastErr error
	var lastResp *http.Response

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		// Respect context before making the call (important on subsequent attempts).
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		resp, err := fn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				// Context cancelled/timed out — surface that immediately.
				return nil, ctx.Err()
			}
			if isRetryableNetError(err) {
				lastErr = err
				lastResp = nil
				if attempt < cfg.MaxAttempts-1 {
					if sleepErr := sleepWithContext(ctx, jitterDelay(cfg, attempt)); sleepErr != nil {
						return nil, sleepErr
					}
				}
				continue
			}
			// Non-retryable network error.
			return nil, err
		}

		// Successful HTTP response — check status.
		if isRetryableStatusCode(resp.StatusCode) {
			// Drain body to allow connection reuse, then close.
			resp.Body.Close()
			lastErr = nil
			lastResp = nil
			if attempt < cfg.MaxAttempts-1 {
				if sleepErr := sleepWithContext(ctx, jitterDelay(cfg, attempt)); sleepErr != nil {
					return nil, sleepErr
				}
			}
			continue
		}

		// Any other status code (including 4xx) — return immediately.
		return resp, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	// All attempts exhausted with retryable HTTP status.
	return lastResp, ErrMaxAttemptsExhausted
}

// ErrMaxAttemptsExhausted is returned when all retry attempts are consumed.
var ErrMaxAttemptsExhausted = &maxAttemptsError{}

type maxAttemptsError struct{}

func (e *maxAttemptsError) Error() string { return "max retry attempts exhausted" }
