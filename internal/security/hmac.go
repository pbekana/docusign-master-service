package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Header names used for HMAC-signed requests.
const (
	HeaderClientID  = "X-Client-ID"
	HeaderTimestamp = "X-Timestamp"
	HeaderNonce     = "X-Nonce"
	HeaderSignature = "X-Signature"
)

// engineerClientID is the well-known read-only client that may only send GET requests.
const engineerClientID = "engineer"

// ReadOnlyViolationError is returned when the engineer client attempts a non-GET request.
type ReadOnlyViolationError struct {
	ClientID string
	Method   string
}

func (e *ReadOnlyViolationError) Error() string {
	return fmt.Sprintf("client %q is read-only and may not use method %s", e.ClientID, e.Method)
}

// IsReadOnlyViolation reports whether err is a ReadOnlyViolationError.
func IsReadOnlyViolation(err error) bool {
	var t *ReadOnlyViolationError
	return errors.As(err, &t)
}

// BuildSignature computes the HMAC-SHA256 signature for a request.
// It is exported for use in tests and the sign_request_example script.
//
// Parameters:
//   - method: HTTP method (e.g. "GET", "POST").
//   - path: request path (e.g. "/api/scan/start").
//   - timestamp: Unix timestamp as decimal string.
//   - nonce: unique request nonce.
//   - body: raw request body bytes.
//   - secret: shared HMAC secret.
//
// Returns the hex-encoded HMAC-SHA256 signature.
func BuildSignature(method, path, timestamp, nonce string, body []byte, secret string) string {
	bodyHash := sha256.Sum256(body)
	bodyHashHex := hex.EncodeToString(bodyHash[:])
	canonical := strings.Join([]string{method, path, timestamp, nonce, bodyHashHex}, "\n")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyRequest authenticates an inbound HTTP request using HMAC-SHA256.
//
// The canonical string is:
//
//	METHOD\nPATH\nTIMESTAMP\nNONCE\nSHA256HEX(BODY)
//
// Parameters:
//   - r: the incoming request (headers are read from it).
//   - body: the raw request body (already read by the caller).
//   - secrets: map of clientID → shared HMAC secret (hex or raw).
//   - toleranceSecs: maximum |now − timestamp| allowed.
//   - nonces: nonce store for replay prevention.
//
// Returns the authenticated clientID on success, or an error.
func VerifyRequest(
	r *http.Request,
	body []byte,
	secrets map[string]string,
	toleranceSecs int64,
	nonces *NonceStore,
) (string, error) {
	clientID := r.Header.Get(HeaderClientID)
	if clientID == "" {
		return "", errors.New("missing X-Client-ID header")
	}

	secret, ok := secrets[clientID]
	if !ok {
		return "", fmt.Errorf("unknown client ID %q", clientID)
	}

	timestampStr := r.Header.Get(HeaderTimestamp)
	if timestampStr == "" {
		return "", errors.New("missing X-Timestamp header")
	}
	tsUnix, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid X-Timestamp %q: %w", timestampStr, err)
	}

	now := time.Now().Unix()
	diff := tsUnix - now
	if diff < 0 {
		diff = -diff
	}
	if diff > toleranceSecs {
		return "", fmt.Errorf("timestamp expired: |now - timestamp| = %ds > tolerance %ds", diff, toleranceSecs)
	}

	nonce := r.Header.Get(HeaderNonce)
	if nonce == "" {
		return "", errors.New("missing X-Nonce header")
	}

	provided := r.Header.Get(HeaderSignature)
	if provided == "" {
		return "", errors.New("missing X-Signature header")
	}

	// Build canonical string.
	bodyHash := sha256.Sum256(body)
	bodyHashHex := hex.EncodeToString(bodyHash[:])
	canonical := strings.Join([]string{
		r.Method,
		r.URL.Path,
		timestampStr,
		nonce,
		bodyHashHex,
	}, "\n")

	// Compute expected HMAC.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	expected := hex.EncodeToString(mac.Sum(nil))

	// Constant-time compare.
	providedBytes, err := hex.DecodeString(provided)
	if err != nil {
		return "", fmt.Errorf("X-Signature is not valid hex: %w", err)
	}
	expectedBytes, _ := hex.DecodeString(expected)
	if !hmac.Equal(expectedBytes, providedBytes) {
		return "", errors.New("signature mismatch")
	}

	// Check read-only constraint for engineer client.
	if strings.EqualFold(clientID, engineerClientID) && r.Method != http.MethodGet {
		return "", &ReadOnlyViolationError{ClientID: clientID, Method: r.Method}
	}

	// Record nonce — expiry is now + 2*tolerance so the cleanup goroutine
	// keeps it long enough to catch any request within the tolerance window.
	expiry := time.Unix(now+2*toleranceSecs, 0)
	if !nonces.Use(nonce, expiry) {
		return "", errors.New("nonce already used (replay detected)")
	}

	return clientID, nil
}
