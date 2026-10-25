package audit

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// AuditService provides high-level audit logging helpers.
// All methods are fire-and-forget: they record errors to slog without
// propagating them to callers so that audit failures never break the request path.
type AuditService struct {
	repo   *AuditRepository
	logger *slog.Logger
}

// NewAuditService creates a new AuditService.
func NewAuditService(repo *AuditRepository, logger *slog.Logger) *AuditService {
	return &AuditService{repo: repo, logger: logger}
}

// LogRequest records an inbound HTTP request outcome.
func (s *AuditService) LogRequest(
	ctx context.Context,
	r *http.Request,
	clientID, orgID string,
	statusCode int,
	outcome, errDetail string,
) {
	severity := SeverityInfo
	if outcome == OutcomeFailure || outcome == OutcomeDenied {
		severity = SeverityWarning
	}
	if statusCode >= 500 {
		severity = SeverityError
	}

	log := AuditLog{
		ID:             uuid.New(),
		EventCategory:  EventCategoryAPI,
		EventType:      "HTTP_REQUEST",
		ActorClientID:  clientID,
		OrganizationID: orgID,
		HTTPMethod:     r.Method,
		Endpoint:       r.URL.Path,
		RequestIP:      r.RemoteAddr,
		StatusCode:     &statusCode,
		Outcome:        outcome,
		Severity:       severity,
		ErrorDetail:    errDetail,
		CreatedAt:      time.Now().UTC(),
	}
	go s.write(context.Background(), log)
}

// LogJobEvent records a job lifecycle event.
func (s *AuditService) LogJobEvent(
	ctx context.Context,
	jobID, orgID, eventType, outcome string,
) {
	severity := SeverityInfo
	if outcome == OutcomeFailure {
		severity = SeverityError
	}

	log := AuditLog{
		ID:             uuid.New(),
		EventCategory:  EventCategoryJob,
		EventType:      eventType,
		OrganizationID: orgID,
		ResourceType:   "JOB",
		ResourceID:     jobID,
		Outcome:        outcome,
		Severity:       severity,
		CreatedAt:      time.Now().UTC(),
	}
	go s.write(context.Background(), log)
}

// LogAuthEvent records an authentication event.
func (s *AuditService) LogAuthEvent(
	ctx context.Context,
	clientID, outcome, errDetail string,
) {
	severity := SeverityInfo
	if outcome == OutcomeFailure || outcome == OutcomeDenied {
		severity = SeverityError
	}

	log := AuditLog{
		ID:            uuid.New(),
		EventCategory: EventCategoryAuth,
		EventType:     "AUTH_ATTEMPT",
		ActorClientID: clientID,
		Outcome:       outcome,
		Severity:      severity,
		ErrorDetail:   errDetail,
		CreatedAt:     time.Now().UTC(),
	}
	go s.write(context.Background(), log)
}

// write persists the audit log, logging any error via slog.
func (s *AuditService) write(ctx context.Context, log AuditLog) {
	if err := s.repo.Insert(ctx, log); err != nil {
		s.logger.Error("audit write failed",
			"event_type", log.EventType,
			"error", err.Error(),
		)
	}
}
