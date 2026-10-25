// Package audit provides audit logging for the DocuSign Master Service.
package audit

import (
	"time"

	"github.com/google/uuid"
)

// Event category constants.
const (
	EventCategoryAuth    = "AUTH"
	EventCategoryJob     = "JOB"
	EventCategoryAPI     = "API"
	EventCategoryStorage = "STORAGE"
	EventCategorySystem  = "SYSTEM"
)

// Outcome constants.
const (
	OutcomeSuccess = "SUCCESS"
	OutcomeFailure = "FAILURE"
	OutcomeDenied  = "DENIED"
)

// Severity constants.
const (
	SeverityInfo    = "INFO"
	SeverityWarning = "WARNING"
	SeverityError   = "ERROR"
)

// AuditLog represents a single audit event. It maps to the audit_logs table.
// The table schema (FEAT-002 migration) has: id, job_id, organization_id,
// event_type, actor, details (jsonb), ip_address, user_agent, created_at.
// We extend the in-memory model with additional fields that are serialised into
// the details jsonb column so the existing table schema is not broken.
type AuditLog struct {
	ID             uuid.UUID              `json:"id"`
	EventCategory  string                 `json:"event_category"`
	EventType      string                 `json:"event_type"`
	ActorClientID  string                 `json:"actor_client_id"`
	ActorRole      string                 `json:"actor_role"`
	OrganizationID string                 `json:"organization_id"`
	EntityType     string                 `json:"entity_type"`
	ResourceType   string                 `json:"resource_type"`
	ResourceID     string                 `json:"resource_id"`
	HTTPMethod     string                 `json:"http_method"`
	Endpoint       string                 `json:"endpoint"`
	RequestIP      string                 `json:"request_ip"`
	StatusCode     *int                   `json:"status_code"`
	Outcome        string                 `json:"outcome"`
	Severity       string                 `json:"severity"`
	ErrorDetail    string                 `json:"error_detail"`
	ExtraMetadata  map[string]interface{} `json:"extra_metadata"`
	CreatedAt      time.Time              `json:"created_at"`
}

// AuditFilters holds optional filters for querying audit logs.
type AuditFilters struct {
	OrganizationID string
	EventType      string
	Outcome        string
	From           *time.Time
	To             *time.Time
	Limit          int
	Offset         int
}
