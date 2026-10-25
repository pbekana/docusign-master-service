package handlers

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/your-org/docusign-master-service/internal/jobs"
)

// NormalizationHandler serves normalization management endpoints.
type NormalizationHandler struct {
	svc    JobSvc
	repo   JobRepo
	outDir string // local output base directory
}

// NewNormalizationHandler creates a NormalizationHandler.
func NewNormalizationHandler(svc JobSvc, repo JobRepo, outDir string) *NormalizationHandler {
	return &NormalizationHandler{
		svc:    svc,
		repo:   repo,
		outDir: outDir,
	}
}

// PostNormalize handles POST /api/normalization/{scan_id}/normalize.
// Re-runs normalization and storage steps for an existing job (skips data fetch).
func (h *NormalizationHandler) PostNormalize(w http.ResponseWriter, r *http.Request) {
	jobID, ok := parseScanID(w, r)
	if !ok {
		return
	}

	// Verify the job exists.
	_, err := h.repo.GetByID(r.Context(), jobID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "job not found")
		return
	}

	// Re-enqueue normalization asynchronously.
	if err := h.repo.UpdateStatus(r.Context(), jobID, jobs.StatusNormalizing, "re-normalizing"); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to update job status: "+err.Error())
		return
	}

	go func() {
		// Best-effort: run the full job again. RunJob handles each stage internally.
		_ = h.svc.RunJob(r.Context(), jobID)
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"scan_id": jobID.String(),
		"status":  "normalizing",
	})
}

// GetTables handles GET /api/normalization/{scan_id}/tables.
// Returns the list of output files produced for the job.
func (h *NormalizationHandler) GetTables(w http.ResponseWriter, r *http.Request) {
	scanID := chi.URLParam(r, "scan_id")
	if scanID == "" {
		writeJSONError(w, http.StatusBadRequest, "scan_id is required")
		return
	}

	jobDir := filepath.Join(h.outDir, scanID)
	entries, err := os.ReadDir(jobDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"files": []string{}})
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to read output directory: "+err.Error())
		return
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"files": files})
}

// GetSupportedObjects handles GET /api/normalization/supported-objects.
func (h *NormalizationHandler) GetSupportedObjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"objects": []string{"envelopes", "recipients", "documents", "templates"},
	})
}
