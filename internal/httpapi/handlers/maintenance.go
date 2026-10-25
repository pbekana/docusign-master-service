package handlers

import (
	"encoding/json"
	"net/http"
)

// MaintenanceHandler serves maintenance operation endpoints.
type MaintenanceHandler struct {
	repo               JobRepo
	workerPool         WorkerEnqueuer
	crashTimeoutMinutes int
}

// NewMaintenanceHandler creates a MaintenanceHandler.
func NewMaintenanceHandler(repo JobRepo, workerPool WorkerEnqueuer, crashTimeoutMinutes int) *MaintenanceHandler {
	return &MaintenanceHandler{
		repo:               repo,
		workerPool:         workerPool,
		crashTimeoutMinutes: crashTimeoutMinutes,
	}
}

// cleanupRequest is the body for POST /api/maintenance/cleanup.
type cleanupRequest struct {
	OlderThanDays int      `json:"older_than_days"`
	Status        []string `json:"status"`
}

// PostCleanup handles POST /api/maintenance/cleanup.
func (h *MaintenanceHandler) PostCleanup(w http.ResponseWriter, r *http.Request) {
	var req cleanupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	count, err := h.repo.DeleteOldJobs(r.Context(), req.OlderThanDays, req.Status)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "cleanup failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"deleted_count": count,
	})
}

// PostDetectCrashed handles POST /api/maintenance/detect-crashed.
func (h *MaintenanceHandler) PostDetectCrashed(w http.ResponseWriter, r *http.Request) {
	n, err := h.repo.MarkCrashed(r.Context(), h.crashTimeoutMinutes)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "detect-crashed failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"marked_crashed": n,
	})
}
