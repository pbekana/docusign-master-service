package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/docusign-master-service/internal/audit"
	"github.com/your-org/docusign-master-service/internal/config"
	"github.com/your-org/docusign-master-service/internal/docusign"
	"github.com/your-org/docusign-master-service/internal/normalization"
	"github.com/your-org/docusign-master-service/internal/storage"
)

// JobService orchestrates a full DocuSign data extraction job.
type JobService struct {
	jobRepo    *JobRepository
	cpRepo     *CheckpointRepository
	dsClient   *docusign.DSClient
	localStore *storage.LocalStorage
	minioStore *storage.MinIOStorage
	auditSvc   *audit.AuditService
	logger     *slog.Logger
	cfg        *config.Config
}

// NewJobService creates a JobService with all required dependencies.
// minioStore may be nil if MinIO upload is not used.
func NewJobService(
	jobRepo *JobRepository,
	cpRepo *CheckpointRepository,
	dsClient *docusign.DSClient,
	localStore *storage.LocalStorage,
	minioStore *storage.MinIOStorage,
	auditSvc *audit.AuditService,
	logger *slog.Logger,
	cfg *config.Config,
) *JobService {
	return &JobService{
		jobRepo:    jobRepo,
		cpRepo:     cpRepo,
		dsClient:   dsClient,
		localStore: localStore,
		minioStore: minioStore,
		auditSvc:   auditSvc,
		logger:     logger,
		cfg:        cfg,
	}
}

// RunJob executes the full extraction pipeline for the given job ID.
// Stages:
//   1. FETCHING_ENVELOPES  – paginated envelope listing
//   2. FETCHING_DETAILS    – recipients + documents per envelope (concurrent)
//   3. NORMALIZING         – normalization
//   4. UPLOADING_TO_MINIO  – if job.UploadToMinio
//   5. COMPLETED
func (s *JobService) RunJob(ctx context.Context, jobID uuid.UUID) error {
	// Load job from DB.
	job, err := s.jobRepo.GetByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("RunJob: loading job: %w", err)
	}

	s.logger.Info("job starting", "job_id", jobID, "org_id", job.OrganizationID)

	// --- Stage 1: Fetch envelopes ---
	if err := s.jobRepo.UpdateStatus(ctx, jobID, StatusFetchingEnvelopes, "envelopes"); err != nil {
		return s.fail(ctx, jobID, err)
	}

	envelopes, err := s.fetchEnvelopes(ctx, job)
	if err != nil {
		if ctx.Err() != nil {
			s.logger.Info("job cancelled during envelope fetch", "job_id", jobID)
			return nil
		}
		return s.fail(ctx, jobID, fmt.Errorf("fetching envelopes: %w", err))
	}

	// --- Stage 2: Fetch details (recipients + documents) ---
	if err := s.jobRepo.UpdateStatus(ctx, jobID, StatusFetchingDetails, "details"); err != nil {
		return s.fail(ctx, jobID, err)
	}

	recipients, documents, err := s.fetchDetails(ctx, job, envelopes)
	if err != nil {
		if ctx.Err() != nil {
			s.logger.Info("job cancelled during detail fetch", "job_id", jobID)
			return nil
		}
		return s.fail(ctx, jobID, fmt.Errorf("fetching details: %w", err))
	}

	// --- Stage 2b: Fetch templates ---
	templates, err := s.fetchTemplates(ctx, job)
	if err != nil {
		if ctx.Err() != nil {
			s.logger.Info("job cancelled during template fetch", "job_id", jobID)
			return nil
		}
		return s.fail(ctx, jobID, fmt.Errorf("fetching templates: %w", err))
	}

	// --- Stage 3: Normalize ---
	if err := s.jobRepo.UpdateStatus(ctx, jobID, StatusNormalizing, "normalizing"); err != nil {
		return s.fail(ctx, jobID, err)
	}

	normEnvelopes := make([]normalization.NormalizedEnvelope, 0, len(envelopes))
	for _, e := range envelopes {
		normEnvelopes = append(normEnvelopes, normalization.NormalizeEnvelope(e, job.DocuSignAccountID, job.OrganizationID))
	}

	normRecipients := make([]normalization.NormalizedRecipient, 0)
	for envID, rr := range recipients {
		normRecipients = append(normRecipients, normalization.NormalizeRecipients(rr, envID, job.DocuSignAccountID, job.OrganizationID)...)
	}

	normDocuments := make([]normalization.NormalizedDocument, 0)
	for envID, dr := range documents {
		normDocuments = append(normDocuments, normalization.NormalizeDocuments(dr, envID, job.DocuSignAccountID, job.OrganizationID)...)
	}

	normTemplates := make([]normalization.NormalizedTemplate, 0, len(templates))
	for _, t := range templates {
		normTemplates = append(normTemplates, normalization.NormalizeTemplate(t, job.DocuSignAccountID, job.OrganizationID))
	}

	// Update counts.
	_ = s.jobRepo.UpdateCounts(ctx, jobID,
		len(normEnvelopes), len(normRecipients), len(normDocuments), len(normTemplates))

	// --- Write output files (local) ---
	processingDate := time.Now().UTC()
	jobIDStr := jobID.String()

	if err := s.writeOutputs(ctx, jobIDStr, normEnvelopes, normRecipients, normDocuments, normTemplates); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return s.fail(ctx, jobID, fmt.Errorf("writing outputs: %w", err))
	}

	// --- Stage 4: Upload to MinIO ---
	if job.UploadToMinio && s.minioStore != nil {
		if err := s.jobRepo.UpdateStatus(ctx, jobID, StatusUploadingToMinio, "uploading"); err != nil {
			return s.fail(ctx, jobID, err)
		}
		if err := s.uploadToMinio(ctx, job, jobIDStr, processingDate, normEnvelopes, normRecipients, normDocuments, normTemplates); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return s.fail(ctx, jobID, fmt.Errorf("uploading to minio: %w", err))
		}
	}

	// --- Done ---
	if err := s.jobRepo.SetCompleted(ctx, jobID); err != nil {
		s.logger.Error("failed to mark job completed", "job_id", jobID, "error", err)
	}

	if s.auditSvc != nil {
		s.auditSvc.LogJobEvent(ctx, jobIDStr, job.OrganizationID, "JOB_COMPLETED", audit.OutcomeSuccess)
	}
	s.logger.Info("job completed",
		"job_id", jobID,
		"envelopes", len(normEnvelopes),
		"recipients", len(normRecipients),
		"documents", len(normDocuments),
		"templates", len(normTemplates),
	)

	// Clean up checkpoints now the job is done.
	_ = s.cpRepo.Delete(ctx, jobID)

	return nil
}

// fetchEnvelopes fetches all envelope pages, resuming from a checkpoint if available.
func (s *JobService) fetchEnvelopes(ctx context.Context, job Job) ([]docusign.RawEnvelope, error) {
	const stageKey = "envelopes"

	cp, err := s.cpRepo.Get(ctx, job.ID, stageKey)
	if err != nil {
		return nil, err
	}

	startPos := 0
	if cp != nil {
		startPos = cp.CursorPosition
	}

	fromDate := job.DateRangeStart.Format(time.RFC3339)
	toDate := job.DateRangeEnd.Format(time.RFC3339)
	pageSize := s.cfg.DocusignPageSize
	if pageSize <= 0 {
		pageSize = 100
	}

	var allEnvelopes []docusign.RawEnvelope
	pos := startPos

	for {
		if ctx.Err() != nil {
			return allEnvelopes, nil
		}

		resp, err := s.dsClient.ListEnvelopes(ctx, job.DocuSignAccountID, fromDate, toDate, pos, pageSize)
		if err != nil {
			return allEnvelopes, err
		}

		allEnvelopes = append(allEnvelopes, resp.Envelopes...)

		// Persist checkpoint: next position = endPosition + 1.
		endPos := 0
		if ep, parseErr := strconv.Atoi(resp.EndPosition); parseErr == nil {
			endPos = ep
		}
		nextPos := endPos + 1

		cpErr := s.cpRepo.Upsert(ctx, JobCheckpoint{
			JobID:            job.ID,
			Stage:            stageKey,
			CursorPosition:   nextPos,
			RecordsProcessed: len(allEnvelopes),
			CheckpointAt:     time.Now().UTC(),
		})
		if cpErr != nil {
			s.logger.Warn("checkpoint upsert failed", "error", cpErr)
		}

		_ = s.jobRepo.UpdateCounts(ctx, job.ID, len(allEnvelopes), 0, 0, 0)

		// Decide whether to fetch next page.
		resultSetSize, _ := strconv.Atoi(resp.ResultSetSize)
		if resultSetSize < pageSize || resp.NextUri == "" {
			break
		}
		pos = nextPos
	}

	return allEnvelopes, nil
}

// fetchDetails fetches recipients and documents for each envelope concurrently.
func (s *JobService) fetchDetails(
	ctx context.Context,
	job Job,
	envelopes []docusign.RawEnvelope,
) (map[string]docusign.RecipientsResponse, map[string]docusign.DocumentsResponse, error) {
	const stageKey = "details"
	const checkpointEvery = 50

	maxConcurrency := s.cfg.MaxConcurrentDocusignRequests
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}
	sem := make(chan struct{}, maxConcurrency)

	type detailResult struct {
		envelopeID string
		recipients docusign.RecipientsResponse
		documents  docusign.DocumentsResponse
		err        error
	}

	results := make(chan detailResult, len(envelopes))
	var wg sync.WaitGroup

	for _, env := range envelopes {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		envID := env.EnvelopeID
		go func() {
			defer wg.Done()
			// Acquire semaphore slot.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results <- detailResult{envelopeID: envID, err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			rr, recErr := s.dsClient.GetRecipients(ctx, job.DocuSignAccountID, envID)
			if recErr != nil {
				results <- detailResult{envelopeID: envID, err: recErr}
				return
			}
			dr, docErr := s.dsClient.GetDocuments(ctx, job.DocuSignAccountID, envID)
			if docErr != nil {
				results <- detailResult{envelopeID: envID, err: docErr}
				return
			}
			results <- detailResult{envelopeID: envID, recipients: *rr, documents: *dr}
		}()
	}

	// Close results when all goroutines finish.
	go func() {
		wg.Wait()
		close(results)
	}()

	allRecipients := make(map[string]docusign.RecipientsResponse, len(envelopes))
	allDocuments := make(map[string]docusign.DocumentsResponse, len(envelopes))
	processed := 0
	var firstErr error

	for result := range results {
		if result.err != nil {
			if ctx.Err() != nil {
				return allRecipients, allDocuments, nil
			}
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		allRecipients[result.envelopeID] = result.recipients
		allDocuments[result.envelopeID] = result.documents
		processed++

		// Checkpoint every 50 envelopes.
		if processed%checkpointEvery == 0 {
			_ = s.cpRepo.Upsert(ctx, JobCheckpoint{
				JobID:            job.ID,
				Stage:            stageKey,
				RecordsProcessed: processed,
				CheckpointAt:     time.Now().UTC(),
			})
		}
	}

	return allRecipients, allDocuments, firstErr
}

// fetchTemplates fetches all template pages.
func (s *JobService) fetchTemplates(ctx context.Context, job Job) ([]docusign.RawTemplate, error) {
	pageSize := s.cfg.DocusignPageSize
	if pageSize <= 0 {
		pageSize = 100
	}

	var allTemplates []docusign.RawTemplate
	pos := 0

	for {
		if ctx.Err() != nil {
			return allTemplates, nil
		}

		resp, err := s.dsClient.ListTemplates(ctx, job.DocuSignAccountID, pos, pageSize)
		if err != nil {
			return allTemplates, err
		}

		allTemplates = append(allTemplates, resp.EnvelopeTemplates...)

		resultSetSize, _ := strconv.Atoi(resp.ResultSetSize)
		if resultSetSize < pageSize {
			break
		}

		endPos, _ := strconv.Atoi(resp.EndPosition)
		pos = endPos + 1
	}

	return allTemplates, nil
}

// writeOutputs serialises normalized data to local storage.
func (s *JobService) writeOutputs(
	ctx context.Context,
	jobIDStr string,
	envelopes []normalization.NormalizedEnvelope,
	recipients []normalization.NormalizedRecipient,
	documents []normalization.NormalizedDocument,
	templates []normalization.NormalizedTemplate,
) error {
	// Write Parquet files.
	envParquet, err := storage.WriteParquet[normalization.NormalizedEnvelope](envelopes)
	if err != nil {
		return fmt.Errorf("writing envelope parquet: %w", err)
	}
	if _, err := s.localStore.WriteParquet(ctx, jobIDStr, "envelopes", envParquet); err != nil {
		return err
	}

	recParquet, err := storage.WriteParquet[normalization.NormalizedRecipient](recipients)
	if err != nil {
		return fmt.Errorf("writing recipient parquet: %w", err)
	}
	if _, err := s.localStore.WriteParquet(ctx, jobIDStr, "recipients", recParquet); err != nil {
		return err
	}

	docParquet, err := storage.WriteParquet[normalization.NormalizedDocument](documents)
	if err != nil {
		return fmt.Errorf("writing document parquet: %w", err)
	}
	if _, err := s.localStore.WriteParquet(ctx, jobIDStr, "documents", docParquet); err != nil {
		return err
	}

	tmplParquet, err := storage.WriteParquet[normalization.NormalizedTemplate](templates)
	if err != nil {
		return fmt.Errorf("writing template parquet: %w", err)
	}
	if _, err := s.localStore.WriteParquet(ctx, jobIDStr, "templates", tmplParquet); err != nil {
		return err
	}

	// Write JSON files.
	envJSON, _ := json.Marshal(envelopes)
	if _, err := s.localStore.WriteJSON(ctx, jobIDStr, "envelopes", envJSON); err != nil {
		return err
	}

	recJSON, _ := json.Marshal(recipients)
	if _, err := s.localStore.WriteJSON(ctx, jobIDStr, "recipients", recJSON); err != nil {
		return err
	}

	docJSON, _ := json.Marshal(documents)
	if _, err := s.localStore.WriteJSON(ctx, jobIDStr, "documents", docJSON); err != nil {
		return err
	}

	tmplJSON, _ := json.Marshal(templates)
	if _, err := s.localStore.WriteJSON(ctx, jobIDStr, "templates", tmplJSON); err != nil {
		return err
	}

	return nil
}

// uploadToMinio uploads all parquet files to MinIO.
func (s *JobService) uploadToMinio(
	ctx context.Context,
	job Job,
	jobIDStr string,
	processingDate time.Time,
	envelopes []normalization.NormalizedEnvelope,
	recipients []normalization.NormalizedRecipient,
	documents []normalization.NormalizedDocument,
	templates []normalization.NormalizedTemplate,
) error {
	if err := s.minioStore.EnsureBucket(ctx); err != nil {
		return err
	}

	type upload struct {
		dataType string
		data     []byte
	}

	uploads := []upload{}

	envParquet, err := storage.WriteParquet[normalization.NormalizedEnvelope](envelopes)
	if err != nil {
		return err
	}
	uploads = append(uploads, upload{"envelopes", envParquet})

	recParquet, err := storage.WriteParquet[normalization.NormalizedRecipient](recipients)
	if err != nil {
		return err
	}
	uploads = append(uploads, upload{"recipients", recParquet})

	docParquet, err := storage.WriteParquet[normalization.NormalizedDocument](documents)
	if err != nil {
		return err
	}
	uploads = append(uploads, upload{"documents", docParquet})

	tmplParquet, err := storage.WriteParquet[normalization.NormalizedTemplate](templates)
	if err != nil {
		return err
	}
	uploads = append(uploads, upload{"templates", tmplParquet})

	for _, u := range uploads {
		key := s.minioStore.ObjectKey(job.OrganizationID, u.dataType, processingDate)
		if err := s.minioStore.UploadObject(ctx, key, u.data, "application/octet-stream"); err != nil {
			return fmt.Errorf("uploading %s: %w", u.dataType, err)
		}
	}
	return nil
}

// fail records an error on the job and returns it.
func (s *JobService) fail(ctx context.Context, jobID uuid.UUID, err error) error {
	s.logger.Error("job failed", "job_id", jobID, "error", err)
	_ = s.jobRepo.SetError(context.Background(), jobID, err.Error())
	return err
}
