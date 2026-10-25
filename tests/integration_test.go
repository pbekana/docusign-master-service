// Package tests contains integration tests for the DocuSign Master Service HTTP API.
// All tests use mock implementations — no live database, no live DocuSign API.
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/docusign-master-service/internal/audit"
	"github.com/your-org/docusign-master-service/internal/config"
	"github.com/your-org/docusign-master-service/internal/httpapi"
	"github.com/your-org/docusign-master-service/internal/httpapi/handlers"
	apimw "github.com/your-org/docusign-master-service/internal/httpapi/middleware"
	"github.com/your-org/docusign-master-service/internal/jobs"
	"github.com/your-org/docusign-master-service/internal/security"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

// mockJobRepo is an in-memory implementation of handlers.JobRepo.
type mockJobRepo struct {
	mu   sync.Mutex
	jobs map[uuid.UUID]*jobs.Job
}

func newMockJobRepo() *mockJobRepo {
	return &mockJobRepo{jobs: make(map[uuid.UUID]*jobs.Job)}
}

func (r *mockJobRepo) Create(_ context.Context, req jobs.StartJobRequest) (jobs.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := jobs.Job{
		ID:                uuid.New(),
		OrganizationID:    req.OrganizationID,
		DocuSignAccountID: req.DocuSignAccountID,
		Status:            jobs.StatusPending,
		DateRangeStart:    req.DateRangeFrom,
		DateRangeEnd:      req.DateRangeTo,
		OutputFormat:      req.OutputFormat,
		UploadToMinio:     req.UploadToMinio,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	r.jobs[job.ID] = &job
	return job, nil
}

func (r *mockJobRepo) GetByID(_ context.Context, id uuid.UUID) (jobs.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return jobs.Job{}, fmt.Errorf("job %s not found", id)
	}
	return *j, nil
}

func (r *mockJobRepo) UpdateStatus(_ context.Context, id uuid.UUID, status, stage string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok {
		j.Status = status
		j.Stage = stage
	}
	return nil
}

func (r *mockJobRepo) SetCancelled(_ context.Context, id uuid.UUID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok {
		j.Status = jobs.StatusCancelled
		j.CancellationReason = reason
	}
	return nil
}

func (r *mockJobRepo) List(_ context.Context, _ string, _, _ int) ([]jobs.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]jobs.Job, 0, len(r.jobs))
	for _, j := range r.jobs {
		result = append(result, *j)
	}
	return result, nil
}

func (r *mockJobRepo) CountByStatus(_ context.Context) (map[string]int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := make(map[string]int)
	for _, j := range r.jobs {
		counts[j.Status]++
	}
	return counts, nil
}

func (r *mockJobRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.jobs, id)
	return nil
}

func (r *mockJobRepo) MarkCrashed(_ context.Context, _ int) (int, error) {
	return 0, nil
}

func (r *mockJobRepo) DeleteOldJobs(_ context.Context, _ int, _ []string) (int, error) {
	return 0, nil
}

// mockWorkerPool is an in-memory implementation of handlers.WorkerEnqueuer.
type mockWorkerPool struct {
	mu      sync.Mutex
	queued  []uuid.UUID
	failEnq bool
}

func (w *mockWorkerPool) Enqueue(jobID uuid.UUID) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failEnq {
		return fmt.Errorf("queue full")
	}
	w.queued = append(w.queued, jobID)
	return nil
}

// mockJobSvc is a no-op implementation of handlers.JobSvc.
type mockJobSvc struct{}

func (s *mockJobSvc) RunJob(_ context.Context, _ uuid.UUID) error {
	return nil
}

// mockAuditSvc is a no-op implementation of handlers.AuditSvc.
type mockAuditSvc struct{}

func (s *mockAuditSvc) LogJobEvent(_ context.Context, _, _, _, _ string) {}
func (s *mockAuditSvc) LogRequest(_ context.Context, _ *http.Request, _, _ string, _ int, _, _ string) {
}
func (s *mockAuditSvc) LogAuthEvent(_ context.Context, _, _, _ string) {}

// mockDocuSignAuth is a test implementation of handlers.DocuSignAuth.
type mockDocuSignAuth struct {
	shouldFail bool
}

func (m *mockDocuSignAuth) ValidateCredentials(_ context.Context) error {
	if m.shouldFail {
		return fmt.Errorf("invalid credentials")
	}
	return nil
}

func (m *mockDocuSignAuth) AccountInfo() (string, string) {
	return "test-account-id", "https://na1.docusign.net"
}

// mockAuditRepo is an in-memory implementation of handlers.AuditQuerier.
type mockAuditRepo struct{}

func (r *mockAuditRepo) QueryLogs(_ context.Context, _ audit.AuditFilters) ([]audit.AuditLog, error) {
	return []audit.AuditLog{}, nil
}

func (r *mockAuditRepo) QueryStats(_ context.Context, _ string) (map[string]int, error) {
	return map[string]int{"SUCCESS": 5}, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

const testHMACSecret = "test-integration-secret-key-12345"
const coordinatorClientID = "coordinator"
const engineerClientID = "engineer"

// buildTestConfig returns a minimal config for tests.
func buildTestConfig() *config.Config {
	return &config.Config{
		HMACSecretKeyCore:             testHMACSecret,
		HMACTimestampToleranceSeconds: 300,
		Port:                          "8080",
		LocalOutputDir:                "/tmp/docusign-test-output",
		CrashTimeoutMinutes:           10,
	}
}

// buildRouter creates a test router wired with all mock dependencies.
func buildRouter(t *testing.T, repo *mockJobRepo, pool *mockWorkerPool) http.Handler {
	t.Helper()

	cfg := buildTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	nonces := security.NewNonceStore(ctx)

	auditSvc := &mockAuditSvc{}
	dsAuth := &mockDocuSignAuth{}
	auditRepo := &mockAuditRepo{}
	jobSvc := &mockJobSvc{}

	healthH := handlers.NewHealthHandler("test", nil, repo)
	credH := handlers.NewCredentialsHandler(dsAuth)
	scanH := handlers.NewScanHandler(jobSvc, repo, pool, auditSvc)
	normH := handlers.NewNormalizationHandler(jobSvc, repo, cfg.LocalOutputDir)
	maintH := handlers.NewMaintenanceHandler(repo, pool, cfg.CrashTimeoutMinutes)
	auditH := handlers.NewAuditHandler(auditSvc, auditRepo)

	return httpapi.NewRouter(cfg, nonces, nil, healthH, credH, scanH, normH, maintH, auditH)
}

// signRequest signs the request with the given client ID and secret.
func signRequest(t *testing.T, req *http.Request, body []byte, clientID, secret string) {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := fmt.Sprintf("nonce-%d-%s", time.Now().UnixNano(), clientID)
	sig := security.BuildSignature(req.Method, req.URL.Path, ts, nonce, body, secret)

	req.Header.Set(security.HeaderClientID, clientID)
	req.Header.Set(security.HeaderTimestamp, ts)
	req.Header.Set(security.HeaderNonce, nonce)
	req.Header.Set(security.HeaderSignature, sig)
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

func TestIntegration(t *testing.T) {
	repo := newMockJobRepo()
	pool := &mockWorkerPool{}

	router := buildRouter(t, repo, pool)

	// Pre-seed a job for status/cancel/resume/remove tests.
	seededJob, _ := repo.Create(context.Background(), jobs.StartJobRequest{
		OrganizationID:    "org-seed",
		DocuSignAccountID: "account-seed",
		DateRangeFrom:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		DateRangeTo:       time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
	})
	// Mark as COMPLETED so we can test DELETE.
	_ = repo.UpdateStatus(context.Background(), seededJob.ID, jobs.StatusCompleted, "")

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		clientID   string
		sign       bool
		wantStatus int
		check      func(t *testing.T, resp *http.Response, body []byte)
	}{
		{
			name:       "GET /api/health — no auth required, returns 200",
			method:     http.MethodGet,
			path:       "/api/health",
			sign:       false, // public endpoint
			wantStatus: http.StatusOK,
			check: func(t *testing.T, resp *http.Response, body []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(body, &data); err != nil {
					t.Fatalf("decode health response: %v", err)
				}
				if data["status"] != "ok" {
					t.Errorf("health status: want ok, got %v", data["status"])
				}
				if data["version"] != "test" {
					t.Errorf("health version: want test, got %v", data["version"])
				}
			},
		},
		{
			name:       "POST /api/scan/start with valid coordinator HMAC returns 202",
			method:     http.MethodPost,
			path:       "/api/scan/start",
			body:       `{"organization_id":"org-1","docusign_account_id":"acct-1","date_range_from":"2024-01-01T00:00:00Z","date_range_to":"2024-12-31T00:00:00Z"}`,
			clientID:   coordinatorClientID,
			sign:       true,
			wantStatus: http.StatusAccepted,
			check: func(t *testing.T, resp *http.Response, body []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(body, &data); err != nil {
					t.Fatalf("decode start response: %v", err)
				}
				if data["scan_id"] == "" {
					t.Error("expected scan_id in response")
				}
				if data["status"] != jobs.StatusPending {
					t.Errorf("expected status PENDING, got %v", data["status"])
				}
			},
		},
		{
			name:       "POST /api/scan/start with invalid HMAC returns 401",
			method:     http.MethodPost,
			path:       "/api/scan/start",
			body:       `{"organization_id":"org-1","date_range_from":"2024-01-01T00:00:00Z","date_range_to":"2024-12-31T00:00:00Z"}`,
			clientID:   coordinatorClientID,
			sign:       false, // deliberately omit signature headers
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "POST /api/scan/start with engineer client (read-only) returns 403",
			method:     http.MethodPost,
			path:       "/api/scan/start",
			body:       `{"organization_id":"org-1","date_range_from":"2024-01-01T00:00:00Z","date_range_to":"2024-12-31T00:00:00Z"}`,
			clientID:   engineerClientID,
			sign:       true,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "GET /api/scan/{id}/status with valid HMAC returns job JSON",
			method:     http.MethodGet,
			path:       fmt.Sprintf("/api/scan/%s/status", seededJob.ID),
			clientID:   coordinatorClientID,
			sign:       true,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, resp *http.Response, body []byte) {
				var job jobs.Job
				if err := json.Unmarshal(body, &job); err != nil {
					t.Fatalf("decode job status response: %v", err)
				}
				if job.ID != seededJob.ID {
					t.Errorf("job ID: want %s, got %s", seededJob.ID, job.ID)
				}
			},
		},
		{
			name:       "POST /api/maintenance/detect-crashed with valid HMAC returns 200",
			method:     http.MethodPost,
			path:       "/api/maintenance/detect-crashed",
			clientID:   coordinatorClientID,
			sign:       true,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, resp *http.Response, body []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(body, &data); err != nil {
					t.Fatalf("decode detect-crashed response: %v", err)
				}
				if _, ok := data["marked_crashed"]; !ok {
					t.Error("expected marked_crashed in response")
				}
			},
		},
		{
			name:       "GET /api/audit/logs with valid HMAC returns 200",
			method:     http.MethodGet,
			path:       "/api/audit/logs",
			clientID:   coordinatorClientID,
			sign:       true,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, resp *http.Response, body []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(body, &data); err != nil {
					t.Fatalf("decode audit logs response: %v", err)
				}
				if _, ok := data["logs"]; !ok {
					t.Error("expected logs field in response")
				}
			},
		},
		{
			name:       "GET /api/normalization/supported-objects returns list",
			method:     http.MethodGet,
			path:       "/api/normalization/supported-objects",
			clientID:   coordinatorClientID,
			sign:       true,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, resp *http.Response, body []byte) {
				var data map[string]interface{}
				if err := json.Unmarshal(body, &data); err != nil {
					t.Fatalf("decode supported-objects response: %v", err)
				}
				objects, ok := data["objects"].([]interface{})
				if !ok {
					t.Fatal("expected objects array in response")
				}
				if len(objects) == 0 {
					t.Error("expected at least one supported object")
				}
				// Check all four object types are present.
				objectSet := make(map[string]bool)
				for _, o := range objects {
					objectSet[o.(string)] = true
				}
				for _, expected := range []string{"envelopes", "recipients", "documents", "templates"} {
					if !objectSet[expected] {
						t.Errorf("missing expected object type: %s", expected)
					}
				}
			},
		},
		{
			name:       "DELETE /api/scan/{id}/remove on COMPLETED job returns 204",
			method:     http.MethodDelete,
			path:       fmt.Sprintf("/api/scan/%s/remove", seededJob.ID),
			clientID:   coordinatorClientID,
			sign:       true,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "GET /api/scan/list returns list",
			method:     http.MethodGet,
			path:       "/api/scan/list?org_id=org-1&limit=10&offset=0",
			clientID:   coordinatorClientID,
			sign:       true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "GET /api/scan/statistics returns status counts",
			method:     http.MethodGet,
			path:       "/api/scan/statistics",
			clientID:   coordinatorClientID,
			sign:       true,
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var bodyBytes []byte
			if tc.body != "" {
				bodyBytes = []byte(tc.body)
			}

			req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			if tc.sign && tc.clientID != "" {
				signRequest(t, req, bodyBytes, tc.clientID, testHMACSecret)
			}

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			resp := rr.Result()
			respBody := rr.Body.Bytes()

			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status: want %d, got %d; body: %s",
					tc.wantStatus, resp.StatusCode, string(respBody))
			}

			if tc.check != nil {
				tc.check(t, resp, respBody)
			}
		})
	}
}

// TestRecoveryMiddleware verifies the recovery middleware catches panics.
func TestRecoveryMiddleware(t *testing.T) {
	// Build a handler that panics.
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	// Wrap just the panic handler with the recovery middleware.
	recovered := apimw.Recovery(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rr := httptest.NewRecorder()
	recovered.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 after panic, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode panic response: %v", err)
	}
	if body["error"] != "internal server error" {
		t.Errorf("expected error message 'internal server error', got %q", body["error"])
	}
}
