package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/your-org/docusign-master-service/internal/audit"
)

// AuditHandler serves audit log query endpoints.
type AuditHandler struct {
	svc  AuditSvc
	repo AuditQuerier
}

// NewAuditHandler creates an AuditHandler.
func NewAuditHandler(svc AuditSvc, repo AuditQuerier) *AuditHandler {
	return &AuditHandler{svc: svc, repo: repo}
}

// GetLogs handles GET /api/audit/logs.
func (h *AuditHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filters := audit.AuditFilters{
		OrganizationID: q.Get("org_id"),
		EventType:      q.Get("event_type"),
		Outcome:        q.Get("outcome"),
	}

	if fromStr := q.Get("from"); fromStr != "" {
		t, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid from time: "+err.Error())
			return
		}
		filters.From = &t
	}
	if toStr := q.Get("to"); toStr != "" {
		t, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid to time: "+err.Error())
			return
		}
		filters.To = &t
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 {
		limit = 100
	}
	filters.Limit = limit
	filters.Offset = offset

	logs, err := h.repo.QueryLogs(r.Context(), filters)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to query audit logs: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":   logs,
		"limit":  limit,
		"offset": offset,
	})
}

// GetStats handles GET /api/audit/stats.
func (h *AuditHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")

	stats, err := h.repo.QueryStats(r.Context(), orgID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to query audit stats: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"counts_by_outcome": stats,
	})
}
