package resilience

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// DoWithRateLimit wraps DoWithRetry to handle HTTP 429 Too Many Requests.
//
// On a 429 response:
//   - The attempt does NOT count against retryCfg.MaxAttempts.
//   - If a Retry-After header is present (integer seconds or RFC1123 HTTP-date),
//     the function sleeps that duration, capped at maxWaitSecs.
//   - If the header is absent, it sleeps fallbackDelaySecs.
//   - If the required wait exceeds maxWaitSecs, the function returns the 429 response
//     to the caller so it can persist resumable state.
//
// Context cancellation is respected during all waits.
func DoWithRateLimit(
	ctx context.Context,
	retryCfg RetryConfig,
	maxWaitSecs int,
	fallbackDelaySecs int,
	fn func(ctx context.Context) (*http.Response, error),
) (*http.Response, error) {
	for {
		resp, err := DoWithRetry(ctx, retryCfg, fn)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, ErrMaxAttemptsExhausted
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			// Not a rate-limit response — return as-is.
			return resp, nil
		}

		// Parse Retry-After header.
		waitSecs := fallbackDelaySecs
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			waitSecs = parseRetryAfter(ra, fallbackDelaySecs)
		}

		// Drain and close the 429 body.
		resp.Body.Close()

		// If the required wait exceeds the budget, surface the 429 to the caller.
		if waitSecs > maxWaitSecs {
			// Re-invoke once more to get a fresh response object, but since we
			// already know the server is rate limiting us, return an error with
			// context that the caller can use to schedule a retry.
			return nil, &RateLimitExceededError{RetryAfterSecs: waitSecs, MaxWaitSecs: maxWaitSecs}
		}

		// Sleep and retry — 429 does not consume a retry attempt.
		if sleepErr := sleepWithContext(ctx, time.Duration(waitSecs)*time.Second); sleepErr != nil {
			return nil, sleepErr
		}
	}
}

// RateLimitExceededError is returned when the Retry-After delay exceeds maxWaitSecs.
type RateLimitExceededError struct {
	RetryAfterSecs int
	MaxWaitSecs    int
}

func (e *RateLimitExceededError) Error() string {
	return "rate limit wait " + strconv.Itoa(e.RetryAfterSecs) + "s exceeds max wait " + strconv.Itoa(e.MaxWaitSecs) + "s"
}

// parseRetryAfter parses the Retry-After header value.
// Supports integer-seconds format and RFC1123 HTTP-date format.
// Returns fallback on parse failure.
func parseRetryAfter(value string, fallback int) int {
	// Try integer seconds first.
	if n, err := strconv.Atoi(value); err == nil && n >= 0 {
		return n
	}
	// Try RFC1123 (HTTP-date).
	if t, err := http.ParseTime(value); err == nil {
		secs := int(time.Until(t).Seconds())
		if secs < 0 {
			secs = 0
		}
		return secs
	}
	return fallback
}
