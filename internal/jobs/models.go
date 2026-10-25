// Package jobs provides the job data model, repository, service, and worker pool
// for running DocuSign data extraction jobs.
package jobs

import (
	"time"

	"github.com/google/uuid"
)

// Job status constants.
const (
	StatusPending           = "PENDING"
	StatusFetchingEnvelopes = "FETCHING_ENVELOPES"
	StatusFetchingDetails   = "FETCHING_DETAILS"
	StatusNormalizing       = "NORMALIZING"
	StatusUploadingToMinio  = "UPLOADING_TO_MINIO"
	StatusCompleted         = "COMPLETED"
	StatusFailed            = "FAILED"
	StatusCancelled         = "CANCELLED"
	StatusCrashed           = "CRASHED"
)

// Job represents a single data-extraction run. It maps to the jobs table.
type Job struct {
	ID                  uuid.UUID  `json:"id"`
	OrganizationID      string     `json:"organization_id"`
	DocuSignAccountID   string     `json:"docusign_account_id"`
	Status              string     `json:"status"`
	Stage               string     `json:"stage"`
	DateRangeStart      time.Time  `json:"date_range_start"`
	DateRangeEnd        time.Time  `json:"date_range_end"`
	EnvelopesProcessed  int        `json:"envelopes_processed"`
	RecipientsProcessed int        `json:"recipients_processed"`
	DocumentsProcessed  int        `json:"documents_processed"`
	TemplatesProcessed  int        `json:"templates_processed"`
	StartedAt           *time.Time `json:"started_at"`
	CompletedAt         *time.Time `json:"completed_at"`
	LastHeartbeatAt     *time.Time `json:"last_heartbeat_at"`
	ErrorMessage        string     `json:"error_message"`
	ErrorCode           string     `json:"error_code"`
	CancelledAt         *time.Time `json:"cancelled_at"`
	CancellationReason  string     `json:"cancellation_reason"`
	OutputFormat        string     `json:"output_format"`
	UploadToMinio       bool       `json:"upload_to_minio"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// JobCheckpoint records the progress of a job stage so a job can be resumed.
// It maps to the job_checkpoints table.
type JobCheckpoint struct {
	ID               uuid.UUID `json:"id"`
	JobID            uuid.UUID `json:"job_id"`
	Stage            string    `json:"stage"`
	CursorPosition   int       `json:"cursor_position"`
	RecordsProcessed int       `json:"records_processed"`
	LastEnvelopeID   string    `json:"last_envelope_id"`
	CheckpointAt     time.Time `json:"checkpoint_at"`
}

// StartJobRequest holds the parameters needed to create a new job.
type StartJobRequest struct {
	OrganizationID    string
	DocuSignAccountID string
	DateRangeFrom     time.Time
	DateRangeTo       time.Time
	OutputFormat      string
	UploadToMinio     bool
}
