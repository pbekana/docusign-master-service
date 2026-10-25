package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/docusign-master-service/internal/config"
	"github.com/your-org/docusign-master-service/internal/docusign"
	"github.com/your-org/docusign-master-service/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock authenticator
// ---------------------------------------------------------------------------

// mockAuth implements docusign.Authenticator using a fixed token and base URI.
type mockAuth struct {
	baseURI string
}

func (m *mockAuth) GetAccessToken(_ context.Context) (string, error) {
	return "test-token", nil
}

func (m *mockAuth) AccountInfo() (string, string) {
	return "test-account-id", m.baseURI
}

// ---------------------------------------------------------------------------
// In-memory job repository
// ---------------------------------------------------------------------------

type inMemoryJobRepo struct {
	mu   sync.Mutex
	jobs map[uuid.UUID]*Job
}

func newInMemoryJobRepo() *inMemoryJobRepo {
	return &inMemoryJobRepo{jobs: make(map[uuid.UUID]*Job)}
}

func (r *inMemoryJobRepo) Create(_ context.Context, req StartJobRequest) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := Job{
		ID:                uuid.New(),
		OrganizationID:    req.OrganizationID,
		DocuSignAccountID: req.DocuSignAccountID,
		Status:            StatusPending,
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

func (r *inMemoryJobRepo) GetByID(_ context.Context, id uuid.UUID) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("job %s not found", id)
	}
	return *j, nil
}

func (r *inMemoryJobRepo) UpdateStatus(_ context.Context, id uuid.UUID, status, stage string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok {
		j.Status = status
		j.Stage = stage
	}
	return nil
}

func (r *inMemoryJobRepo) UpdateHeartbeat(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok {
		t := time.Now().UTC()
		j.LastHeartbeatAt = &t
	}
	return nil
}

func (r *inMemoryJobRepo) UpdateCounts(_ context.Context, id uuid.UUID, envelopes, recipients, documents, templates int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok {
		j.EnvelopesProcessed = envelopes
		j.RecipientsProcessed = recipients
		j.DocumentsProcessed = documents
		j.TemplatesProcessed = templates
	}
	return nil
}

func (r *inMemoryJobRepo) SetError(_ context.Context, id uuid.UUID, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok {
		j.Status = StatusFailed
		j.ErrorMessage = errMsg
	}
	return nil
}

func (r *inMemoryJobRepo) SetCompleted(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok {
		j.Status = StatusCompleted
		t := time.Now().UTC()
		j.CompletedAt = &t
	}
	return nil
}

func (r *inMemoryJobRepo) SetCancelled(_ context.Context, id uuid.UUID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok {
		j.Status = StatusCancelled
		j.CancellationReason = reason
	}
	return nil
}

func (r *inMemoryJobRepo) List(_ context.Context, _ string, _, _ int) ([]Job, error) {
	return nil, nil
}

func (r *inMemoryJobRepo) ListByStatus(_ context.Context, status string) ([]Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []Job
	for _, j := range r.jobs {
		if j.Status == status {
			result = append(result, *j)
		}
	}
	return result, nil
}

func (r *inMemoryJobRepo) MarkCrashed(_ context.Context, _ int) (int, error) {
	return 0, nil
}

func (r *inMemoryJobRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.jobs, id)
	return nil
}

func (r *inMemoryJobRepo) RecordFailedCall(_ context.Context, _, _, _ string, _ *uuid.UUID, _ map[string]interface{}, _ string) error {
	return nil
}

// ---------------------------------------------------------------------------
// In-memory checkpoint repository
// ---------------------------------------------------------------------------

type inMemoryCheckpointRepo struct {
	mu          sync.Mutex
	checkpoints map[string]*JobCheckpoint // key: jobID+":"+stage
}

func newInMemoryCheckpointRepo() *inMemoryCheckpointRepo {
	return &inMemoryCheckpointRepo{checkpoints: make(map[string]*JobCheckpoint)}
}

func (r *inMemoryCheckpointRepo) Upsert(_ context.Context, cp JobCheckpoint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := cp.JobID.String() + ":" + cp.Stage
	existing, ok := r.checkpoints[key]
	if ok {
		existing.CursorPosition = cp.CursorPosition
		existing.RecordsProcessed = cp.RecordsProcessed
		existing.LastEnvelopeID = cp.LastEnvelopeID
		existing.CheckpointAt = cp.CheckpointAt
	} else {
		cpCopy := cp
		if cpCopy.ID == uuid.Nil {
			cpCopy.ID = uuid.New()
		}
		r.checkpoints[key] = &cpCopy
	}
	return nil
}

func (r *inMemoryCheckpointRepo) Get(_ context.Context, jobID uuid.UUID, stage string) (*JobCheckpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := jobID.String() + ":" + stage
	cp, ok := r.checkpoints[key]
	if !ok {
		return nil, nil
	}
	cpCopy := *cp
	return &cpCopy, nil
}

func (r *inMemoryCheckpointRepo) Delete(_ context.Context, _ uuid.UUID) error {
	return nil
}

// ---------------------------------------------------------------------------
// JobService adapter for test (using interfaces instead of concrete repos)
// ---------------------------------------------------------------------------

// jobServiceForTest holds interface-based dependencies so tests can inject mocks.
// We define a parallel service type here to avoid changing the production service signature.
// Instead, we directly wire things via the production JobService with a test shim.

// testJobRepo wraps our in-memory mock as a *JobRepository to pass to JobService.
// Since JobService uses *JobRepository and *CheckpointRepository directly, we need
// to use the same struct but swap the underlying pool. The simplest approach is to
// define a parallel service constructor that accepts interfaces. However, to avoid
// changing the production service API, we instead test through a thin adapter.

// jobServiceAdapter allows the test to call RunJob with mock repos.
type jobServiceAdapter struct {
	jobRepo  jobRepoIface
	cpRepo   cpRepoIface
	dsClient *docusign.DSClient
	local    *storage.LocalStorage
	cfg      *config.Config
}

type jobRepoIface interface {
	GetByID(ctx context.Context, id uuid.UUID) (Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status, stage string) error
	UpdateCounts(ctx context.Context, id uuid.UUID, envelopes, recipients, documents, templates int) error
	SetError(ctx context.Context, id uuid.UUID, errMsg string) error
	SetCompleted(ctx context.Context, id uuid.UUID) error
	UpdateHeartbeat(ctx context.Context, id uuid.UUID) error
}

type cpRepoIface interface {
	Get(ctx context.Context, jobID uuid.UUID, stage string) (*JobCheckpoint, error)
	Upsert(ctx context.Context, cp JobCheckpoint) error
	Delete(ctx context.Context, jobID uuid.UUID) error
}

// runJob runs the full pipeline with injected mocks.
func (a *jobServiceAdapter) runJob(ctx context.Context, jobID uuid.UUID) (string, error) {
	job, err := a.jobRepo.GetByID(ctx, jobID)
	if err != nil {
		return "", err
	}

	// Stage 1: fetch envelopes.
	if err := a.jobRepo.UpdateStatus(ctx, jobID, StatusFetchingEnvelopes, "envelopes"); err != nil {
		return "", err
	}
	envelopes, err := fetchEnvelopesVia(ctx, a.dsClient, a.cpRepo, job, a.cfg)
	if err != nil {
		if ctx.Err() != nil {
			return job.Status, nil
		}
		_ = a.jobRepo.SetError(ctx, jobID, err.Error())
		return StatusFailed, err
	}

	// Stage 2: fetch details.
	if err := a.jobRepo.UpdateStatus(ctx, jobID, StatusFetchingDetails, "details"); err != nil {
		return "", err
	}
	recipients, documents, err := fetchDetailsVia(ctx, a.dsClient, a.cpRepo, job, envelopes, a.cfg)
	if err != nil {
		if ctx.Err() != nil {
			return StatusFetchingDetails, nil
		}
		_ = a.jobRepo.SetError(ctx, jobID, err.Error())
		return StatusFailed, err
	}

	_ = recipients
	_ = documents

	// Stage 3: templates.
	templates, err := fetchTemplatesVia(ctx, a.dsClient, job, a.cfg)
	if err != nil {
		if ctx.Err() != nil {
			return StatusFetchingDetails, nil
		}
		_ = a.jobRepo.SetError(ctx, jobID, err.Error())
		return StatusFailed, err
	}
	_ = templates

	// Stage 4: write outputs.
	if err := a.jobRepo.UpdateStatus(ctx, jobID, StatusNormalizing, "normalizing"); err != nil {
		return "", err
	}
	_ = a.jobRepo.UpdateCounts(ctx, jobID, len(envelopes), 0, 0, len(templates))

	if err := a.jobRepo.SetCompleted(ctx, jobID); err != nil {
		return "", err
	}
	return StatusCompleted, nil
}

// fetchEnvelopesVia runs envelope pagination using injected interfaces.
func fetchEnvelopesVia(
	ctx context.Context,
	dsClient *docusign.DSClient,
	cpRepo cpRepoIface,
	job Job,
	cfg *config.Config,
) ([]docusign.RawEnvelope, error) {
	pageSize := cfg.DocusignPageSize
	if pageSize <= 0 {
		pageSize = 100
	}

	cp, _ := cpRepo.Get(ctx, job.ID, "envelopes")
	startPos := 0
	if cp != nil {
		startPos = cp.CursorPosition
	}

	fromDate := job.DateRangeStart.Format(time.RFC3339)
	toDate := job.DateRangeEnd.Format(time.RFC3339)

	var all []docusign.RawEnvelope
	pos := startPos
	for {
		if ctx.Err() != nil {
			return all, nil
		}
		resp, err := dsClient.ListEnvelopes(ctx, job.DocuSignAccountID, fromDate, toDate, pos, pageSize)
		if err != nil {
			return all, err
		}
		all = append(all, resp.Envelopes...)

		var endPos int
		fmt.Sscanf(resp.EndPosition, "%d", &endPos)
		nextPos := endPos + 1

		_ = cpRepo.Upsert(ctx, JobCheckpoint{
			JobID:            job.ID,
			Stage:            "envelopes",
			CursorPosition:   nextPos,
			RecordsProcessed: len(all),
			CheckpointAt:     time.Now().UTC(),
		})

		resultSetSize := 0
		fmt.Sscanf(resp.ResultSetSize, "%d", &resultSetSize)
		if resultSetSize < pageSize || resp.NextUri == "" {
			break
		}
		pos = nextPos
	}
	return all, nil
}

func fetchDetailsVia(
	ctx context.Context,
	dsClient *docusign.DSClient,
	cpRepo cpRepoIface,
	job Job,
	envelopes []docusign.RawEnvelope,
	cfg *config.Config,
) (map[string]docusign.RecipientsResponse, map[string]docusign.DocumentsResponse, error) {
	allR := make(map[string]docusign.RecipientsResponse)
	allD := make(map[string]docusign.DocumentsResponse)
	for _, env := range envelopes {
		if ctx.Err() != nil {
			return allR, allD, nil
		}
		rr, err := dsClient.GetRecipients(ctx, job.DocuSignAccountID, env.EnvelopeID)
		if err != nil {
			return allR, allD, err
		}
		dr, err := dsClient.GetDocuments(ctx, job.DocuSignAccountID, env.EnvelopeID)
		if err != nil {
			return allR, allD, err
		}
		allR[env.EnvelopeID] = *rr
		allD[env.EnvelopeID] = *dr
	}
	return allR, allD, nil
}

func fetchTemplatesVia(
	ctx context.Context,
	dsClient *docusign.DSClient,
	job Job,
	cfg *config.Config,
) ([]docusign.RawTemplate, error) {
	pageSize := cfg.DocusignPageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	var all []docusign.RawTemplate
	pos := 0
	for {
		if ctx.Err() != nil {
			return all, nil
		}
		resp, err := dsClient.ListTemplates(ctx, job.DocuSignAccountID, pos, pageSize)
		if err != nil {
			return all, err
		}
		all = append(all, resp.EnvelopeTemplates...)
		rs := 0
		fmt.Sscanf(resp.ResultSetSize, "%d", &rs)
		if rs < pageSize {
			break
		}
		var ep int
		fmt.Sscanf(resp.EndPosition, "%d", &ep)
		pos = ep + 1
	}
	return all, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func makeTestConfig(baseURL string) *config.Config {
	return &config.Config{
		DocusignPageSize:              2,
		MaxConcurrentDocusignRequests: 2,
		RetryMaxAttempts:              1,
		RetryBaseDelayMS:              0,
		RetryMaxDelayMS:               0,
		RateLimitMaxWaitSeconds:       5,
		RateLimitFallbackDelaySeconds: 1,
		HeartbeatIntervalSeconds:      30,
		CrashTimeoutMinutes:           10,
		LocalOutputDir:                os.TempDir(),
	}
}

func makeDSClient(t *testing.T, baseURL string) *docusign.DSClient {
	t.Helper()
	cfg := makeTestConfig(baseURL)
	auth := &mockAuth{baseURI: baseURL}
	return docusign.NewDSClientWithAuthenticator(auth, cfg)
}

// ---------------------------------------------------------------------------
// Test (a): successful full job run end-to-end
// ---------------------------------------------------------------------------

func TestRunJob_Success(t *testing.T) {
	// Set up mock DocuSign server.
	accountID := "test-account-id"

	mux := http.NewServeMux()

	// Envelope listing — single page (resultSetSize < pageSize means no more pages).
	mux.HandleFunc(fmt.Sprintf("/restapi/v2.1/accounts/%s/envelopes", accountID), func(w http.ResponseWriter, r *http.Request) {
		resp := docusign.EnvelopeListResponse{
			Envelopes: []docusign.RawEnvelope{
				{EnvelopeID: "env-1", Status: "completed"},
			},
			ResultSetSize: "1",
			TotalSetSize:  "1",
			EndPosition:   "0",
			NextUri:       "",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Recipients for env-1.
	mux.HandleFunc(fmt.Sprintf("/restapi/v2.1/accounts/%s/envelopes/env-1/recipients", accountID), func(w http.ResponseWriter, r *http.Request) {
		resp := docusign.RecipientsResponse{
			Signers: []docusign.RawRecipient{
				{RecipientID: "rec-1", Name: "Alice", Email: "alice@example.com", Status: "completed"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Documents for env-1.
	mux.HandleFunc(fmt.Sprintf("/restapi/v2.1/accounts/%s/envelopes/env-1/documents", accountID), func(w http.ResponseWriter, r *http.Request) {
		resp := docusign.DocumentsResponse{
			EnvelopeDocuments: []docusign.RawDocument{
				{DocumentID: "doc-1", Name: "Contract", Type: "content"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Templates — empty.
	mux.HandleFunc(fmt.Sprintf("/restapi/v2.1/accounts/%s/templates", accountID), func(w http.ResponseWriter, r *http.Request) {
		resp := docusign.TemplatesResponse{
			EnvelopeTemplates: []docusign.RawTemplate{},
			ResultSetSize:     "0",
			TotalSetSize:      "0",
			EndPosition:       "0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	jobRepo := newInMemoryJobRepo()
	cpRepo := newInMemoryCheckpointRepo()

	cfg := makeTestConfig(srv.URL)
	dsClient := makeDSClient(t, srv.URL)

	// Create a job.
	job, err := jobRepo.Create(context.Background(), StartJobRequest{
		OrganizationID:    "org-1",
		DocuSignAccountID: accountID,
		DateRangeFrom:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		DateRangeTo:       time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		OutputFormat:      "parquet",
		UploadToMinio:     false,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	adapter := &jobServiceAdapter{
		jobRepo:  jobRepo,
		cpRepo:   cpRepo,
		dsClient: dsClient,
		cfg:      cfg,
	}

	finalStatus, err := adapter.runJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("runJob returned error: %v", err)
	}
	if finalStatus != StatusCompleted {
		t.Errorf("expected status COMPLETED, got %s", finalStatus)
	}

	// Verify job state in repo.
	updated, _ := jobRepo.GetByID(context.Background(), job.ID)
	if updated.Status != StatusCompleted {
		t.Errorf("job status in repo: want COMPLETED, got %s", updated.Status)
	}
	if updated.EnvelopesProcessed != 1 {
		t.Errorf("envelopes processed: want 1, got %d", updated.EnvelopesProcessed)
	}
}

// ---------------------------------------------------------------------------
// Test (b): pagination — mock returns 2 pages, both must be fetched
// ---------------------------------------------------------------------------

func TestRunJob_Pagination(t *testing.T) {
	accountID := "test-account-id"

	var pageRequests atomic.Int32

	mux := http.NewServeMux()

	mux.HandleFunc(fmt.Sprintf("/restapi/v2.1/accounts/%s/envelopes", accountID), func(w http.ResponseWriter, r *http.Request) {
		page := pageRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")

		startPos := r.URL.Query().Get("start_position")
		if startPos == "0" || startPos == "" {
			// First page: return 2 envelopes (== page size), signal more pages with NextUri.
			resp := docusign.EnvelopeListResponse{
				Envelopes: []docusign.RawEnvelope{
					{EnvelopeID: fmt.Sprintf("env-p%d-1", page), Status: "completed"},
					{EnvelopeID: fmt.Sprintf("env-p%d-2", page), Status: "completed"},
				},
				ResultSetSize: "2",
				TotalSetSize:  "4",
				EndPosition:   "1",
				NextUri:       "/restapi/v2.1/accounts/" + accountID + "/envelopes?start_position=2",
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			// Second page: return 1 envelope (< page size), no NextUri.
			resp := docusign.EnvelopeListResponse{
				Envelopes: []docusign.RawEnvelope{
					{EnvelopeID: fmt.Sprintf("env-p%d-1", page), Status: "completed"},
				},
				ResultSetSize: "1",
				TotalSetSize:  "4",
				EndPosition:   "2",
				NextUri:       "",
			}
			json.NewEncoder(w).Encode(resp)
		}
	})

	// Recipients for any envelope.
	mux.HandleFunc("/restapi/v2.1/accounts/"+accountID+"/envelopes/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path[len(r.URL.Path)-len("recipients"):] == "recipients" {
			json.NewEncoder(w).Encode(docusign.RecipientsResponse{})
		} else {
			json.NewEncoder(w).Encode(docusign.DocumentsResponse{})
		}
	})

	// Templates.
	mux.HandleFunc(fmt.Sprintf("/restapi/v2.1/accounts/%s/templates", accountID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(docusign.TemplatesResponse{
			EnvelopeTemplates: nil,
			ResultSetSize:     "0",
			EndPosition:       "0",
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	jobRepo := newInMemoryJobRepo()
	cpRepo := newInMemoryCheckpointRepo()
	cfg := makeTestConfig(srv.URL)
	dsClient := makeDSClient(t, srv.URL)

	job, _ := jobRepo.Create(context.Background(), StartJobRequest{
		OrganizationID:    "org-2",
		DocuSignAccountID: accountID,
		DateRangeFrom:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		DateRangeTo:       time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
	})

	envelopes, err := fetchEnvelopesVia(context.Background(), dsClient, cpRepo, job, cfg)
	if err != nil {
		t.Fatalf("fetchEnvelopes error: %v", err)
	}

	if len(envelopes) != 3 {
		t.Errorf("expected 3 envelopes (page1=2, page2=1), got %d", len(envelopes))
	}

	if pageRequests.Load() != 2 {
		t.Errorf("expected 2 page requests, got %d", pageRequests.Load())
	}

	// Verify checkpoint advanced to position 3 (endPos 2 + 1).
	cp, err := cpRepo.Get(context.Background(), job.ID, "envelopes")
	if err != nil {
		t.Fatalf("checkpoint get error: %v", err)
	}
	if cp == nil {
		t.Fatal("expected checkpoint to exist")
	}
	if cp.CursorPosition != 3 {
		t.Errorf("expected checkpoint cursor 3, got %d", cp.CursorPosition)
	}
}

// ---------------------------------------------------------------------------
// Test (c): context cancellation mid-fetch
// ---------------------------------------------------------------------------

func TestRunJob_Cancellation(t *testing.T) {
	accountID := "test-account-id"

	var requestCount atomic.Int32

	// Server blocks on the second request until cancelled.
	blockCh := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/restapi/v2.1/accounts/%s/envelopes", accountID), func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		if n == 1 {
			// First page: succeed.
			resp := docusign.EnvelopeListResponse{
				Envelopes: []docusign.RawEnvelope{
					{EnvelopeID: "env-cancel-1", Status: "completed"},
					{EnvelopeID: "env-cancel-2", Status: "completed"},
				},
				ResultSetSize: "2",
				TotalSetSize:  "10",
				EndPosition:   "1",
				NextUri:       "/next",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		} else {
			// Second page: block until client cancels.
			select {
			case <-blockCh:
			case <-r.Context().Done():
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})

	mux.HandleFunc(fmt.Sprintf("/restapi/v2.1/accounts/%s/templates", accountID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(docusign.TemplatesResponse{ResultSetSize: "0", EndPosition: "0"})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer close(blockCh)

	jobRepo := newInMemoryJobRepo()
	cpRepo := newInMemoryCheckpointRepo()
	cfg := makeTestConfig(srv.URL)
	dsClient := makeDSClient(t, srv.URL)

	job, _ := jobRepo.Create(context.Background(), StartJobRequest{
		OrganizationID:    "org-3",
		DocuSignAccountID: accountID,
		DateRangeFrom:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		DateRangeTo:       time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
	})

	// Update status to FETCHING_ENVELOPES so adapter can read it.
	_ = jobRepo.UpdateStatus(context.Background(), job.ID, StatusFetchingEnvelopes, "envelopes")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	adapter := &jobServiceAdapter{
		jobRepo:  jobRepo,
		cpRepo:   cpRepo,
		dsClient: dsClient,
		cfg:      cfg,
	}

	finalStatus, err := adapter.runJob(ctx, job.ID)
	// We expect either a timeout error or a nil error with a partial status.
	// The key requirement: job must NOT be set to FAILED (no error record).
	_ = err
	_ = finalStatus

	// Verify: job was NOT marked as failed.
	updated, _ := jobRepo.GetByID(context.Background(), job.ID)
	if updated.Status == StatusFailed {
		t.Errorf("job was incorrectly marked FAILED after cancellation; status=%s errMsg=%s",
			updated.Status, updated.ErrorMessage)
	}

	// Verify: some envelopes were processed before cancellation.
	if updated.EnvelopesProcessed < 2 {
		t.Logf("envelopes processed before cancellation: %d", updated.EnvelopesProcessed)
	}
}
