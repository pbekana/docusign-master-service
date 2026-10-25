package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/your-org/docusign-master-service/internal/audit"
	"github.com/your-org/docusign-master-service/internal/config"
	"github.com/your-org/docusign-master-service/internal/security"
)

// contextKey is an unexported type for context values set by this package.
type contextKey int

const clientIDKey contextKey = 0

// ClientIDFromContext extracts the authenticated client ID from the request context.
// Returns an empty string if not present.
func ClientIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(clientIDKey).(string)
	return v
}

// withClientID returns a copy of ctx with the given client ID stored.
func withClientID(ctx context.Context, clientID string) context.Context {
	return context.WithValue(ctx, clientIDKey, clientID)
}

// HMAC returns middleware that authenticates requests using HMAC-SHA256 request signing.
// The paths /api/health and /api/stats are skipped.
// Both secrets (core + engineer) from cfg are accepted.
// On success the authenticated clientID is stored in the request context.
func HMAC(cfg *config.Config, nonces *security.NonceStore, auditSvc *audit.AuditService) func(http.Handler) http.Handler {
	secrets := buildSecrets(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip public endpoints.
			if r.URL.Path == "/api/health" || strings.HasPrefix(r.URL.Path, "/api/stats") {
				next.ServeHTTP(w, r)
				return
			}

			// Buffer the body so both this middleware and handlers can read it.
			bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 8<<20)) // 8 MiB max
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "failed to read request body")
				return
			}
			// Restore body for downstream handlers.
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			clientID, verifyErr := security.VerifyRequest(r, bodyBytes, secrets, cfg.HMACTimestampToleranceSeconds, nonces)
			if verifyErr != nil {
				if auditSvc != nil {
					auditSvc.LogAuthEvent(r.Context(), r.Header.Get(security.HeaderClientID), audit.OutcomeFailure, verifyErr.Error())
				}

				if security.IsReadOnlyViolation(verifyErr) {
					slog.WarnContext(r.Context(), "read-only client attempted mutation",
						"client_id", r.Header.Get(security.HeaderClientID),
						"method", r.Method,
						"path", r.URL.Path,
					)
					writeJSONError(w, http.StatusForbidden, "client is read-only and may not use this method")
					return
				}

				slog.WarnContext(r.Context(), "HMAC authentication failed",
					"reason", verifyErr.Error(),
					"path", r.URL.Path,
				)
				writeJSONError(w, http.StatusUnauthorized, verifyErr.Error())
				return
			}

			// Store client ID in context.
			ctx := withClientID(r.Context(), clientID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// buildSecrets constructs the map[clientID]secret used by VerifyRequest.
// Currently supports a single "core" client and a read-only "engineer" client
// that shares the same secret (in this implementation both use HMACSecretKeyCore).
func buildSecrets(cfg *config.Config) map[string]string {
	secrets := make(map[string]string)
	if cfg.HMACSecretKeyCore != "" {
		secrets["coordinator"] = cfg.HMACSecretKeyCore
		secrets["engineer"] = cfg.HMACSecretKeyCore
	}
	return secrets
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
