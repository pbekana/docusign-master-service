package handlers

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/your-org/docusign-master-service/internal/audit"
	"github.com/your-org/docusign-master-service/internal/jobs"
)

// JobRepo is satisfied by *jobs.JobRepository. Defined here so handlers can
// accept mock implementations in integration tests.
type JobRepo interface {
	Create(ctx context.Context, req jobs.StartJobRequest) (jobs.Job, error)
	GetByID(ctx context.Context, id uuid.UUID) (jobs.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status, stage string) error
	SetCancelled(ctx context.Context, id uuid.UUID, reason string) error
	List(ctx context.Context, orgID string, limit, offset int) ([]jobs.Job, error)
	CountByStatus(ctx context.Context) (map[string]int, error)
	Delete(ctx context.Context, id uuid.UUID) error
	MarkCrashed(ctx context.Context, crashTimeoutMinutes int) (int, error)
	DeleteOldJobs(ctx context.Context, olderThanDays int, statuses []string) (int, error)
}

// JobSvc is satisfied by *jobs.JobService. Defined here for testability.
type JobSvc interface {
	RunJob(ctx context.Context, jobID uuid.UUID) error
}

// WorkerEnqueuer is a narrow interface from *jobs.WorkerPool.
type WorkerEnqueuer interface {
	Enqueue(jobID uuid.UUID) error
}

// AuditSvc is satisfied by *audit.AuditService.
type AuditSvc interface {
	LogJobEvent(ctx context.Context, jobID, orgID, eventType, outcome string)
	LogRequest(ctx context.Context, r *http.Request, clientID, orgID string, statusCode int, outcome, errDetail string)
	LogAuthEvent(ctx context.Context, clientID, outcome, errDetail string)
}

// DocuSignAuth is a narrow interface from *docusign.JWTAuthClient.
type DocuSignAuth interface {
	ValidateCredentials(ctx context.Context) error
	AccountInfo() (accountID, baseURI string)
}

// AuditQuerier is satisfied by *audit.AuditRepository.
type AuditQuerier interface {
	QueryLogs(ctx context.Context, filters audit.AuditFilters) ([]audit.AuditLog, error)
	QueryStats(ctx context.Context, orgID string) (map[string]int, error)
}
