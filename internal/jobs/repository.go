package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// JobRepository persists and retrieves jobs and failed external call records.
type JobRepository struct {
	pool *pgxpool.Pool
}

// NewJobRepository creates a new JobRepository.
func NewJobRepository(pool *pgxpool.Pool) *JobRepository {
	return &JobRepository{pool: pool}
}

// Create inserts a new job record and returns it with the server-assigned ID and timestamps.
func (r *JobRepository) Create(ctx context.Context, req StartJobRequest) (Job, error) {
	outputFormat := req.OutputFormat
	if outputFormat == "" {
		outputFormat = "parquet"
	}

	var job Job
	err := r.pool.QueryRow(ctx, `
		INSERT INTO jobs (
			organization_id, docusign_account_id, status,
			date_range_start, date_range_end,
			envelopes_processed, recipients_processed, documents_processed, templates_processed,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 0, 0, 0, 0, NOW(), NOW())
		RETURNING id, organization_id, docusign_account_id, status, COALESCE(stage,''),
		          date_range_start, date_range_end,
		          envelopes_processed, recipients_processed, documents_processed, templates_processed,
		          started_at, completed_at, last_heartbeat_at,
		          COALESCE(error_message,''), COALESCE(error_code,''),
		          cancelled_at, COALESCE(cancellation_reason,''),
		          created_at, updated_at
	`,
		req.OrganizationID,
		req.DocuSignAccountID,
		StatusPending,
		req.DateRangeFrom,
		req.DateRangeTo,
	).Scan(
		&job.ID, &job.OrganizationID, &job.DocuSignAccountID, &job.Status, &job.Stage,
		&job.DateRangeStart, &job.DateRangeEnd,
		&job.EnvelopesProcessed, &job.RecipientsProcessed, &job.DocumentsProcessed, &job.TemplatesProcessed,
		&job.StartedAt, &job.CompletedAt, &job.LastHeartbeatAt,
		&job.ErrorMessage, &job.ErrorCode,
		&job.CancelledAt, &job.CancellationReason,
		&job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return Job{}, fmt.Errorf("job repository: create: %w", err)
	}
	job.OutputFormat = outputFormat
	job.UploadToMinio = req.UploadToMinio
	return job, nil
}

// GetByID retrieves a single job by its primary key.
func (r *JobRepository) GetByID(ctx context.Context, id uuid.UUID) (Job, error) {
	var job Job
	err := r.pool.QueryRow(ctx, `
		SELECT id, organization_id, docusign_account_id, status, COALESCE(stage,''),
		       date_range_start, date_range_end,
		       envelopes_processed, recipients_processed, documents_processed, templates_processed,
		       started_at, completed_at, last_heartbeat_at,
		       COALESCE(error_message,''), COALESCE(error_code,''),
		       cancelled_at, COALESCE(cancellation_reason,''),
		       created_at, updated_at
		FROM jobs WHERE id = $1
	`, id).Scan(
		&job.ID, &job.OrganizationID, &job.DocuSignAccountID, &job.Status, &job.Stage,
		&job.DateRangeStart, &job.DateRangeEnd,
		&job.EnvelopesProcessed, &job.RecipientsProcessed, &job.DocumentsProcessed, &job.TemplatesProcessed,
		&job.StartedAt, &job.CompletedAt, &job.LastHeartbeatAt,
		&job.ErrorMessage, &job.ErrorCode,
		&job.CancelledAt, &job.CancellationReason,
		&job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Job{}, fmt.Errorf("job repository: job %s not found", id)
		}
		return Job{}, fmt.Errorf("job repository: get by id: %w", err)
	}
	return job, nil
}

// UpdateStatus sets the job status and optional stage field.
func (r *JobRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status, stage string) error {
	var err error
	if status == StatusFetchingEnvelopes || status == StatusPending {
		// Set started_at on first active status.
		_, err = r.pool.Exec(ctx, `
			UPDATE jobs SET status=$1, stage=$2, started_at=COALESCE(started_at,NOW()), updated_at=NOW()
			WHERE id=$3
		`, status, stage, id)
	} else {
		_, err = r.pool.Exec(ctx, `
			UPDATE jobs SET status=$1, stage=$2, updated_at=NOW()
			WHERE id=$3
		`, status, stage, id)
	}
	if err != nil {
		return fmt.Errorf("job repository: update status: %w", err)
	}
	return nil
}

// UpdateHeartbeat records a heartbeat timestamp.
func (r *JobRepository) UpdateHeartbeat(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs SET last_heartbeat_at=NOW(), updated_at=NOW() WHERE id=$1
	`, id)
	if err != nil {
		return fmt.Errorf("job repository: update heartbeat: %w", err)
	}
	return nil
}

// UpdateCounts sets the processed record counts.
func (r *JobRepository) UpdateCounts(ctx context.Context, id uuid.UUID, envelopes, recipients, documents, templates int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs
		SET envelopes_processed=$1, recipients_processed=$2, documents_processed=$3, templates_processed=$4, updated_at=NOW()
		WHERE id=$5
	`, envelopes, recipients, documents, templates, id)
	if err != nil {
		return fmt.Errorf("job repository: update counts: %w", err)
	}
	return nil
}

// SetError records an error and marks the job as FAILED.
func (r *JobRepository) SetError(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs SET status=$1, error_message=$2, updated_at=NOW() WHERE id=$3
	`, StatusFailed, errMsg, id)
	if err != nil {
		return fmt.Errorf("job repository: set error: %w", err)
	}
	return nil
}

// SetCompleted marks the job as COMPLETED with a completion timestamp.
func (r *JobRepository) SetCompleted(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs SET status=$1, completed_at=NOW(), updated_at=NOW() WHERE id=$2
	`, StatusCompleted, id)
	if err != nil {
		return fmt.Errorf("job repository: set completed: %w", err)
	}
	return nil
}

// SetCancelled marks the job as CANCELLED.
func (r *JobRepository) SetCancelled(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs SET status=$1, cancelled_at=NOW(), cancellation_reason=$2, updated_at=NOW() WHERE id=$3
	`, StatusCancelled, reason, id)
	if err != nil {
		return fmt.Errorf("job repository: set cancelled: %w", err)
	}
	return nil
}

// List returns jobs for an organization with pagination.
func (r *JobRepository) List(ctx context.Context, orgID string, limit, offset int) ([]Job, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, organization_id, docusign_account_id, status, COALESCE(stage,''),
		       date_range_start, date_range_end,
		       envelopes_processed, recipients_processed, documents_processed, templates_processed,
		       started_at, completed_at, last_heartbeat_at,
		       COALESCE(error_message,''), COALESCE(error_code,''),
		       cancelled_at, COALESCE(cancellation_reason,''),
		       created_at, updated_at
		FROM jobs WHERE organization_id=$1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, orgID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("job repository: list: %w", err)
	}
	defer rows.Close()
	return scanJobs(rows)
}

// ListByStatus returns all jobs with the given status.
func (r *JobRepository) ListByStatus(ctx context.Context, status string) ([]Job, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, organization_id, docusign_account_id, status, COALESCE(stage,''),
		       date_range_start, date_range_end,
		       envelopes_processed, recipients_processed, documents_processed, templates_processed,
		       started_at, completed_at, last_heartbeat_at,
		       COALESCE(error_message,''), COALESCE(error_code,''),
		       cancelled_at, COALESCE(cancellation_reason,''),
		       created_at, updated_at
		FROM jobs WHERE status=$1
		ORDER BY created_at ASC
	`, status)
	if err != nil {
		return nil, fmt.Errorf("job repository: list by status: %w", err)
	}
	defer rows.Close()
	return scanJobs(rows)
}

// MarkCrashed transitions stale in-progress jobs to CRASHED.
// A job is considered stale if its last_heartbeat_at is older than crashTimeoutMinutes.
// Returns the number of jobs marked as crashed.
func (r *JobRepository) MarkCrashed(ctx context.Context, crashTimeoutMinutes int) (int, error) {
	if crashTimeoutMinutes <= 0 {
		crashTimeoutMinutes = 10
	}
	tag, err := r.pool.Exec(ctx, fmt.Sprintf(`
		UPDATE jobs
		SET status='CRASHED', updated_at=NOW()
		WHERE status IN ('FETCHING_ENVELOPES','FETCHING_DETAILS','NORMALIZING','UPLOADING_TO_MINIO')
		  AND last_heartbeat_at < NOW() - INTERVAL '%d minutes'
	`, crashTimeoutMinutes))
	if err != nil {
		return 0, fmt.Errorf("job repository: mark crashed: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// Delete removes a job and its cascading child records.
func (r *JobRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("job repository: delete: %w", err)
	}
	return nil
}

// RecordFailedCall persists an exhausted external call failure into the
// failed_external_calls table (migration 003, which uses the name failed_calls).
func (r *JobRepository) RecordFailedCall(
	ctx context.Context,
	targetService, operation, orgID string,
	jobID *uuid.UUID,
	payload map[string]interface{},
	errMsg string,
) error {
	payloadJSON, _ := json.Marshal(payload)

	_, err := r.pool.Exec(ctx, `
		INSERT INTO failed_calls (job_id, service, method, url, request_body, error_message, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`,
		jobID,
		targetService,
		operation,
		orgID,
		string(payloadJSON),
		errMsg,
	)
	if err != nil {
		return fmt.Errorf("job repository: record failed call: %w", err)
	}
	return nil
}

// scanJobs is a helper that scans a pgx.Rows result into a []Job slice.
func scanJobs(rows pgx.Rows) ([]Job, error) {
	var jobs []Job
	for rows.Next() {
		var job Job
		if err := rows.Scan(
			&job.ID, &job.OrganizationID, &job.DocuSignAccountID, &job.Status, &job.Stage,
			&job.DateRangeStart, &job.DateRangeEnd,
			&job.EnvelopesProcessed, &job.RecipientsProcessed, &job.DocumentsProcessed, &job.TemplatesProcessed,
			&job.StartedAt, &job.CompletedAt, &job.LastHeartbeatAt,
			&job.ErrorMessage, &job.ErrorCode,
			&job.CancelledAt, &job.CancellationReason,
			&job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("job repository: scanning row: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("job repository: iterating rows: %w", err)
	}
	return jobs, nil
}

// CountByStatus returns a map of status -> count for all jobs.
func (r *JobRepository) CountByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT status, COUNT(*) FROM jobs GROUP BY status
	`)
	if err != nil {
		return nil, fmt.Errorf("job repository: count by status: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("job repository: scanning count row: %w", err)
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("job repository: iterating count rows: %w", err)
	}
	return counts, nil
}

// DeleteOldJobs removes jobs older than olderThanDays with an optional status filter.
// Returns the number of deleted jobs.
func (r *JobRepository) DeleteOldJobs(ctx context.Context, olderThanDays int, statuses []string) (int, error) {
	if olderThanDays <= 0 {
		olderThanDays = 30
	}

	var tag interface{ RowsAffected() int64 }
	var err error

	if len(statuses) > 0 {
		// Build $N placeholders for the status list.
		args := []interface{}{olderThanDays}
		placeholders := make([]string, len(statuses))
		for i, s := range statuses {
			args = append(args, s)
			placeholders[i] = fmt.Sprintf("$%d", i+2)
		}
		query := fmt.Sprintf(`
			DELETE FROM jobs
			WHERE created_at < NOW() - ($1 || ' days')::interval
			  AND status IN (%s)
		`, strings.Join(placeholders, ","))
		tag, err = r.pool.Exec(ctx, query, args...)
	} else {
		tag, err = r.pool.Exec(ctx, `
			DELETE FROM jobs
			WHERE created_at < NOW() - ($1 || ' days')::interval
		`, olderThanDays)
	}

	if err != nil {
		return 0, fmt.Errorf("job repository: delete old jobs: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// now is a package-level alias used in tests to inject deterministic time.
var now = func() time.Time { return time.Now().UTC() }
