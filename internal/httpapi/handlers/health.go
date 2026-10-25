// Package handlers provides HTTP request handlers for the DocuSign Master Service.
package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthHandler serves health and stats endpoints.
type HealthHandler struct {
	version   string
	startTime time.Time
	pool      *pgxpool.Pool
	repo      JobRepo
}

// NewHealthHandler creates a HealthHandler.
func NewHealthHandler(version string, pool *pgxpool.Pool, repo JobRepo) *HealthHandler {
	return &HealthHandler{
		version:   version,
		startTime: time.Now(),
		pool:      pool,
		repo:      repo,
	}
}

// GetHealth handles GET /api/health.
func (h *HealthHandler) GetHealth(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(h.startTime).Seconds()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":         "ok",
		"version":        h.version,
		"uptime_seconds": uptime,
	})
}

// GetStats handles GET /api/stats.
func (h *HealthHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	counts, err := h.repo.CountByStatus(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to fetch stats")
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
