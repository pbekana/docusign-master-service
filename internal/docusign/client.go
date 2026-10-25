package docusign

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/your-org/docusign-master-service/internal/config"
	"github.com/your-org/docusign-master-service/internal/resilience"
)

const maxResponseBytes = 10 * 1024 * 1024 // 10 MB

// DSClient is a reusable HTTP client for the DocuSign eSignature REST API v2.1.
// Use NewDSClient to construct one.
type DSClient struct {
	auth          *JWTAuthClient
	httpClient    *http.Client
	retryCfg      resilience.RetryConfig
	maxWaitSecs   int
	fallbackDelay int
	semaphore     chan struct{}
}

// NewDSClient creates a DSClient from a JWTAuthClient and service config.
func NewDSClient(auth *JWTAuthClient, cfg *config.Config) *DSClient {
	timeout := time.Duration(cfg.DocusignRequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	sem := make(chan struct{}, cfg.MaxConcurrentDocusignRequests)
	if cfg.MaxConcurrentDocusignRequests <= 0 {
		sem = make(chan struct{}, 5)
	}

	return &DSClient{
		auth:       auth,
		httpClient: &http.Client{Timeout: timeout},
		retryCfg: resilience.RetryConfig{
			MaxAttempts: cfg.RetryMaxAttempts,
			BaseDelayMs: cfg.RetryBaseDelayMS,
			MaxDelayMs:  cfg.RetryMaxDelayMS,
		},
		maxWaitSecs:   cfg.RateLimitMaxWaitSeconds,
		fallbackDelay: cfg.RateLimitFallbackDelaySeconds,
		semaphore:     sem,
	}
}

// doRequest performs an authenticated GET request against the DocuSign API,
// JSON-decoding the response body into out.
func (c *DSClient) doRequest(ctx context.Context, method, url string, out interface{}) error {
	// Acquire concurrency slot.
	select {
	case c.semaphore <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.semaphore }()

	// Obtain token outside the retry loop (GetAccessToken caches it).
	token, err := c.auth.GetAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("docusign client: getting access token: %w", err)
	}

	resp, err := resilience.DoWithRateLimit(ctx, c.retryCfg, c.maxWaitSecs, c.fallbackDelay,
		func(ctx context.Context) (*http.Response, error) {
			req, err := http.NewRequestWithContext(ctx, method, url, nil)
			if err != nil {
				return nil, fmt.Errorf("building request: %w", err)
			}
			// token is sensitive — set header, do not log.
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "application/json")
			return c.httpClient.Do(req)
		},
	)
	if err != nil {
		return fmt.Errorf("docusign client: %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.handleHTTPError(resp)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("docusign client: reading response body: %w", err)
	}

	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("docusign client: decoding response: %w", err)
		}
	}
	return nil
}

// handleHTTPError reads the response body and returns a descriptive error.
func (c *DSClient) handleHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("docusign API error: status %d: %s", resp.StatusCode, sanitizeBody(body))
}
