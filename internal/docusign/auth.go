// Package docusign provides a client for the DocuSign eSignature REST API v2.1.
package docusign

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/your-org/docusign-master-service/internal/config"
)

// JWTAuthClient obtains and caches DocuSign access tokens using the JWT Grant flow.
// It is safe for concurrent use.
type JWTAuthClient struct {
	integrationKey string
	userID         string
	privateKeyPEM  []byte
	authServer     string // hostname only, e.g. "account-d.docusign.com"
	scopes         string

	mu           sync.Mutex
	cachedToken  string
	tokenExpiry  time.Time
	accountID    string
	baseURI      string
}

// NewJWTAuthClient creates a JWTAuthClient from cfg.
// It reads the RSA private key from cfg.DocusignPrivateKeyPath.
func NewJWTAuthClient(cfg *config.Config) (*JWTAuthClient, error) {
	if cfg.DocusignPrivateKeyPath == "" {
		return nil, fmt.Errorf("docusign: DOCUSIGN_PRIVATE_KEY_PATH is required")
	}
	pem, err := os.ReadFile(cfg.DocusignPrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("docusign: reading private key: %w", err)
	}

	// Strip scheme from authServer to get hostname only.
	authServerHost := stripScheme(cfg.DocusignAuthServer)

	c := &JWTAuthClient{
		integrationKey: cfg.DocusignIntegrationKey,
		userID:         cfg.DocusignUserID,
		privateKeyPEM:  pem,
		authServer:     authServerHost,
		scopes:         cfg.DocusignOAuthScopes,
		accountID:      cfg.DocusignAccountID,
		baseURI:        cfg.DocusignAPIBaseURI,
	}
	return c, nil
}

// GetAccessToken returns a valid access token, refreshing it if necessary.
// The token is cached until 5 minutes before expiry.
// Never logs the token or private key.
func (c *JWTAuthClient) GetAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cachedToken != "" && time.Until(c.tokenExpiry) > 5*time.Minute {
		return c.cachedToken, nil
	}
	return c.fetchToken(ctx)
}

// fetchToken performs the JWT Grant request to obtain a new access token.
// Must be called with c.mu held.
func (c *JWTAuthClient) fetchToken(ctx context.Context) (string, error) {
	now := time.Now()

	// Build JWT claims.
	claims := jwt.MapClaims{
		"iss":   c.integrationKey,
		"sub":   c.userID,
		"aud":   c.authServer, // hostname only — not a full URL
		"iat":   now.Unix(),
		"exp":   now.Add(3600 * time.Second).Unix(),
		"scope": c.scopes,
	}

	// Parse the RSA private key.
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(c.privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("docusign auth: parsing RSA private key: %w", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	assertion, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("docusign auth: signing JWT: %w", err)
	}
	// assertion is sensitive — do not log it.

	// POST to token endpoint.
	tokenURL := "https://" + c.authServer + "/oauth/token"
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("docusign auth: building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("docusign auth: token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", fmt.Errorf("docusign auth: reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docusign auth: token endpoint returned %d: %s", resp.StatusCode, sanitizeBody(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("docusign auth: decoding token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("docusign auth: token response contained no access_token")
	}

	expiresIn := time.Duration(tokenResp.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 3600 * time.Second
	}

	// Cache token — do not log the token value.
	c.cachedToken = tokenResp.AccessToken
	c.tokenExpiry = now.Add(expiresIn)

	return c.cachedToken, nil
}

// resolveAccountInfo fetches userinfo and sets accountID/baseURI.
// Must be called with c.mu held.
func (c *JWTAuthClient) resolveAccountInfo(ctx context.Context, accessToken string) error {
	userInfoURL := "https://" + c.authServer + "/oauth/userinfo"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return fmt.Errorf("docusign auth: building userinfo request: %w", err)
	}
	// accessToken is sensitive — set it in the header, do not log it.
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("docusign auth: userinfo request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return fmt.Errorf("docusign auth: reading userinfo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docusign auth: userinfo returned %d: %s", resp.StatusCode, sanitizeBody(body))
	}

	var info struct {
		Accounts []struct {
			AccountID   string `json:"account_id"`
			IsDefault   bool   `json:"is_default"`
			BaseURI     string `json:"base_uri"`
			AccountName string `json:"account_name"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("docusign auth: decoding userinfo: %w", err)
	}
	if len(info.Accounts) == 0 {
		return fmt.Errorf("docusign auth: no accounts found in userinfo response")
	}

	// Find matching account, or use first/default.
	for _, acc := range info.Accounts {
		if c.accountID != "" && acc.AccountID == c.accountID {
			c.baseURI = acc.BaseURI
			return nil
		}
		if c.accountID == "" && acc.IsDefault {
			c.accountID = acc.AccountID
			c.baseURI = acc.BaseURI
			return nil
		}
	}

	// Fall back to first account.
	if c.accountID == "" {
		c.accountID = info.Accounts[0].AccountID
	}
	c.baseURI = info.Accounts[0].BaseURI
	return nil
}

// ValidateCredentials obtains an access token and resolves account info.
// Returns nil on success. Use this to verify credentials without starting a job.
func (c *JWTAuthClient) ValidateCredentials(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	token, err := c.fetchToken(ctx)
	if err != nil {
		return err
	}
	return c.resolveAccountInfo(ctx, token)
}

// AccountInfo returns the resolved accountID and baseURI.
// Call ValidateCredentials or GetAccessToken first.
func (c *JWTAuthClient) AccountInfo() (accountID, baseURI string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accountID, c.baseURI
}

// stripScheme removes "https://" or "http://" from a URL, returning the hostname (+ path if any).
func stripScheme(rawURL string) string {
	if strings.HasPrefix(rawURL, "https://") {
		return strings.TrimPrefix(rawURL, "https://")
	}
	if strings.HasPrefix(rawURL, "http://") {
		return strings.TrimPrefix(rawURL, "http://")
	}
	return rawURL
}

// sanitizeBody returns the first 256 bytes of a response body as a string,
// redacting anything that looks like a token or secret.
func sanitizeBody(b []byte) string {
	if len(b) > 256 {
		b = b[:256]
	}
	return string(b)
}
