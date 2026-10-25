package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/your-org/docusign-master-service/internal/audit"
	"github.com/your-org/docusign-master-service/internal/httpapi/middleware"
	"github.com/your-org/docusign-master-service/internal/jobs"
)

// ScanHandler serves scan/job management endpoints.
type ScanHandler struct {
	svc        JobSvc
	repo       JobRepo
	workerPool WorkerEnqueuer
	auditSvc   AuditSvc
}

// NewScanHandler creates a ScanHandler.
func NewScanHandler(
	svc JobSvc,
	repo JobRepo,
	workerPool WorkerEnqueuer,
	auditSvc AuditSvc,
) *ScanHandler {
	return &ScanHandler{
		svc:        svc,
		repo:       repo,
		workerPool: workerPool,
		auditSvc:   auditSvc,
	}
}

// startJobRequest is the JSON body for POST /api/scan/start.
type startJobRequest struct {
	OrganizationID    string `json:"organization_id"`
	DocuSignAccountID string `json:"docusign_account_id"`
	DateRangeFrom     string `json:"date_range_from"` // RFC3339
	DateRangeTo       string `json:"date_range_to"`   // RFC3339
	OutputFormat      string `json:"output_format"`
	UploadToMinio     bool   `json:"upload_to_minio"`
}

// PostStart handles POST /api/scan/start.
func (h *ScanHandler) PostStart(w http.ResponseWriter, r *http.Request) {
	var req startJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.OrganizationID == "" {
		writeJSONError(w, http.StatusBadRequest, "organization_id is required")
		return
	}

	from, err := time.Parse(time.RFC3339, req.DateRangeFrom)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid date_range_from: "+err.Error())
		return
	}
	to, err := time.Parse(time.RFC3339, req.DateRangeTo)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid date_range_to: "+err.Error())
		return
	}

	serviceReq := jobs.StartJobRequest{
		OrganizationID:    req.OrganizationID,
		DocuSignAccountID: req.DocuSignAccountID,
		DateRangeFrom:     from,
		DateRangeTo:       to,
		OutputFormat:      req.OutputFormat,
		UploadToMinio:     req.UploadToMinio,
	}

	job, err := h.repo.Create(r.Context(), serviceReq)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create job: "+err.Error())
		return
	}

	if err := h.workerPool.Enqueue(job.ID); err != nil {
		// Job created but could not be enqueued; it remains PENDING and will be
		// recovered on next startup.
		slogWarn(r, "enqueue failed", "job_id", job.ID, "error", err)
	}

	clientID := middleware.ClientIDFromContext(r.Context())
	if h.auditSvc != nil {
		h.auditSvc.LogJobEvent(r.Context(), job.ID.String(), job.OrganizationID, "JOB_STARTED", audit.OutcomeSuccess)
		h.auditSvc.LogRequest(r.Context(), r, clientID, job.OrganizationID, http.StatusAccepted, audit.OutcomeSuccess, "")
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"scan_id": job.ID.String(),
		"status":  job.Status,
	})
}

// GetStatus handles GET /api/scan/{scan_id}/status.
func (h *ScanHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	jobID, ok := parseScanID(w, r)
	if !ok {
		return
	}

	job, err := h.repo.GetByID(r.Context(), jobID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "job not found")
		return
	}

	writeJSON(w, http.StatusOK, job)
}

// PostCancel handles POST /api/scan/{scan_id}/cancel.
func (h *ScanHandler) PostCancel(w http.ResponseWriter, r *http.Request) {
	jobID, ok := parseScanID(w, r)
	if !ok {
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if err := h.repo.SetCancelled(r.Context(), jobID, body.Reason); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to cancel job: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "CANCELLED"})
}

// PostResume handles POST /api/scan/{scan_id}/resume.
func (h *ScanHandler) PostResume(w http.ResponseWriter, r *http.Request) {
	jobID, ok := parseScanID(w, r)
	if !ok {
		return
	}

	if err := h.repo.UpdateStatus(r.Context(), jobID, jobs.StatusPending, ""); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to resume job: "+err.Error())
		return
	}

	if err := h.workerPool.Enqueue(jobID); err != nil {
		slogWarn(r, "enqueue on resume failed", "job_id", jobID, "error", err)
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"scan_id": jobID.String(),
		"status":  jobs.StatusPending,
	})
}

// GetList handles GET /api/scan/list.
func (h *ScanHandler) GetList(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	if limit <= 0 {
		limit = 50
	}

	jobList, err := h.repo.List(r.Context(), orgID, limit, offset)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list jobs: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs":   jobList,
		"limit":  limit,
		"offset": offset,
	})
}

// GetStatistics handles GET /api/scan/statistics.
func (h *ScanHandler) GetStatistics(w http.ResponseWriter, r *http.Request) {
	counts, err := h.repo.CountByStatus(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to fetch statistics: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"counts_by_status": counts,
	})
}

// DeleteRemove handles DELETE /api/scan/{scan_id}/remove.
func (h *ScanHandler) DeleteRemove(w http.ResponseWriter, r *http.Request) {
	jobID, ok := parseScanID(w, r)
	if !ok {
		return
	}

	job, err := h.repo.GetByID(r.Context(), jobID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "job not found")
		return
	}

	switch job.Status {
	case jobs.StatusCompleted, jobs.StatusFailed, jobs.StatusCancelled:
		// allowed
	default:
		writeJSONError(w, http.StatusConflict, "job must be in COMPLETED, FAILED, or CANCELLED status to remove")
		return
	}

	if err := h.repo.Delete(r.Context(), jobID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to delete job: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parseScanID extracts and parses the {scan_id} URL param.
func parseScanID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := chi.URLParam(r, "scan_id")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid scan_id: "+err.Error())
		return uuid.Nil, false
	}
	return id, true
}

// slogWarn is a convenience helper for structured warnings.
func slogWarn(r *http.Request, msg string, args ...interface{}) {
	slog.WarnContext(r.Context(), msg, args...)
}
