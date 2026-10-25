package security_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/your-org/docusign-master-service/internal/security"
)

const (
	coordinatorID     = "coordinator"
	coordinatorSecret = "super-secret-coordinator-key"
	engineerID        = "engineer"
	engineerSecret    = "super-secret-engineer-key"
	toleranceSecs     = int64(300)
)

var testSecrets = map[string]string{
	coordinatorID: coordinatorSecret,
	engineerID:    engineerSecret,
}

// buildSignedRequest creates an *http.Request with all required HMAC headers.
func buildSignedRequest(
	t *testing.T,
	method, path string,
	body []byte,
	clientID, secret string,
	timestampUnix int64,
	nonce string,
) *http.Request {
	t.Helper()

	tsStr := fmt.Sprintf("%d", timestampUnix)
	bodyHash := sha256.Sum256(body)
	bodyHashHex := hex.EncodeToString(bodyHash[:])
	canonical := strings.Join([]string{method, path, tsStr, nonce, bodyHashHex}, "\n")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	sig := hex.EncodeToString(mac.Sum(nil))

	var bodyReader *strings.Reader
	if len(body) > 0 {
		bodyReader = strings.NewReader(string(body))
	} else {
		bodyReader = strings.NewReader("")
	}

	r := httptest.NewRequest(method, path, bodyReader)
	r.Header.Set(security.HeaderClientID, clientID)
	r.Header.Set(security.HeaderTimestamp, tsStr)
	r.Header.Set(security.HeaderNonce, nonce)
	r.Header.Set(security.HeaderSignature, sig)
	return r
}

func newNonces(t *testing.T) *security.NonceStore {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return security.NewNonceStore(ctx)
}

func TestVerifyRequest(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name        string
		setup       func(nonces *security.NonceStore) (*http.Request, []byte)
		wantClient  string
		wantErrSub  string
		wantROVErr  bool
	}{
		{
			name: "valid coordinator signature on POST",
			setup: func(nonces *security.NonceStore) (*http.Request, []byte) {
				body := []byte(`{"key":"value"}`)
				r := buildSignedRequest(t, http.MethodPost, "/api/v1/jobs", body,
					coordinatorID, coordinatorSecret, now, "nonce-valid-coordinator")
				return r, body
			},
			wantClient: coordinatorID,
		},
		{
			name: "invalid signature rejected",
			setup: func(nonces *security.NonceStore) (*http.Request, []byte) {
				body := []byte(`{"key":"value"}`)
				r := buildSignedRequest(t, http.MethodPost, "/api/v1/jobs", body,
					coordinatorID, "wrong-secret", now, "nonce-bad-sig")
				return r, body
			},
			wantErrSub: "signature mismatch",
		},
		{
			name: "expired timestamp rejected",
			setup: func(nonces *security.NonceStore) (*http.Request, []byte) {
				body := []byte{}
				oldTS := now - toleranceSecs - 10
				r := buildSignedRequest(t, http.MethodGet, "/api/v1/jobs", body,
					coordinatorID, coordinatorSecret, oldTS, "nonce-old-ts")
				return r, body
			},
			wantErrSub: "timestamp expired",
		},
		{
			name: "nonce replay rejected on second use",
			setup: func(nonces *security.NonceStore) (*http.Request, []byte) {
				body := []byte{}
				nonce := "nonce-replay-test"
				// First request — consume the nonce.
				r1 := buildSignedRequest(t, http.MethodGet, "/api/v1/jobs", body,
					coordinatorID, coordinatorSecret, now, nonce)
				if _, err := security.VerifyRequest(r1, body, testSecrets, toleranceSecs, nonces); err != nil {
					t.Fatalf("first request should pass: %v", err)
				}
				// Second request — same nonce, fresh timestamp.
				r2 := buildSignedRequest(t, http.MethodGet, "/api/v1/jobs", body,
					coordinatorID, coordinatorSecret, now, nonce)
				return r2, body
			},
			wantErrSub: "nonce already used",
		},
		{
			name: "unknown client ID rejected",
			setup: func(nonces *security.NonceStore) (*http.Request, []byte) {
				body := []byte{}
				r := buildSignedRequest(t, http.MethodGet, "/api/v1/jobs", body,
					"unknown-client", "any-secret", now, "nonce-unknown-client")
				return r, body
			},
			wantErrSub: "unknown client ID",
		},
		{
			name: "engineer client rejected on POST",
			setup: func(nonces *security.NonceStore) (*http.Request, []byte) {
				body := []byte(`{"key":"value"}`)
				r := buildSignedRequest(t, http.MethodPost, "/api/v1/jobs", body,
					engineerID, engineerSecret, now, "nonce-engineer-post")
				return r, body
			},
			wantErrSub: "read-only",
			wantROVErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nonces := newNonces(t)
			r, body := tc.setup(nonces)

			gotClient, err := security.VerifyRequest(r, body, testSecrets, toleranceSecs, nonces)

			if tc.wantErrSub == "" {
				// Expect success.
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				if gotClient != tc.wantClient {
					t.Errorf("clientID = %q, want %q", gotClient, tc.wantClient)
				}
			} else {
				// Expect a specific error.
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErrSub)
				}
				if tc.wantROVErr && !security.IsReadOnlyViolation(err) {
					t.Errorf("expected ReadOnlyViolationError, got %T: %v", err, err)
				}
			}
		})
	}
}
