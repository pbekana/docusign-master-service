// Package httpapi provides the HTTP router and middleware stack.
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/your-org/docusign-master-service/internal/audit"
	"github.com/your-org/docusign-master-service/internal/config"
	"github.com/your-org/docusign-master-service/internal/httpapi/handlers"
	apimw "github.com/your-org/docusign-master-service/internal/httpapi/middleware"
	"github.com/your-org/docusign-master-service/internal/security"
)

// NewRouter wires the full Chi router with all middleware and routes.
//
// Public endpoints (/api/health, /api/stats) are registered outside the HMAC
// group and require no authentication. All other routes are inside the HMAC
// group and require valid signed requests.
func NewRouter(
	cfg *config.Config,
	nonces *security.NonceStore,
	auditSvc *audit.AuditService,
	healthH *handlers.HealthHandler,
	credH *handlers.CredentialsHandler,
	scanH *handlers.ScanHandler,
	normH *handlers.NormalizationHandler,
	maintH *handlers.MaintenanceHandler,
	auditH *handlers.AuditHandler,
) http.Handler {
	r := chi.NewRouter()

	// Global middleware: recovery, request ID, logging.
	r.Use(apimw.Recovery)
	r.Use(chimw.RequestID)
	r.Use(apimw.Logging)

	// Public endpoints — no HMAC required.
	r.Get("/api/health", healthH.GetHealth)
	r.Get("/api/stats", healthH.GetStats)

	// HMAC-protected group.
	r.Group(func(r chi.Router) {
		r.Use(apimw.HMAC(cfg, nonces, auditSvc))

		// Credential validation.
		r.Post("/api/validate-credentials", credH.PostValidateCredentials)

		// Scan / job management.
		r.Post("/api/scan/start", scanH.PostStart)
		r.Get("/api/scan/list", scanH.GetList)
		r.Get("/api/scan/statistics", scanH.GetStatistics)
		r.Get("/api/scan/{scan_id}/status", scanH.GetStatus)
		r.Post("/api/scan/{scan_id}/cancel", scanH.PostCancel)
		r.Post("/api/scan/{scan_id}/resume", scanH.PostResume)
		r.Delete("/api/scan/{scan_id}/remove", scanH.DeleteRemove)

		// Normalization.
		r.Get("/api/normalization/supported-objects", normH.GetSupportedObjects)
		r.Post("/api/normalization/{scan_id}/normalize", normH.PostNormalize)
		r.Get("/api/normalization/{scan_id}/tables", normH.GetTables)

		// Maintenance.
		r.Post("/api/maintenance/cleanup", maintH.PostCleanup)
		r.Post("/api/maintenance/detect-crashed", maintH.PostDetectCrashed)

		// Audit.
		r.Get("/api/audit/logs", auditH.GetLogs)
		r.Get("/api/audit/stats", auditH.GetStats)
	})

	return r
}
