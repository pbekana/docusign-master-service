package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditRepository provides database access for audit logs.
type AuditRepository struct {
	pool *pgxpool.Pool
}

// NewAuditRepository creates a new AuditRepository.
func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

// Insert persists an AuditLog. Fields beyond the base schema are serialised into
// the details jsonb column so the existing migration schema is preserved.
func (r *AuditRepository) Insert(ctx context.Context, log AuditLog) error {
	if log.ID == uuid.Nil {
		log.ID = uuid.New()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}

	// Compose details jsonb from extended fields.
	details := map[string]interface{}{
		"event_category": log.EventCategory,
		"actor_role":     log.ActorRole,
		"entity_type":    log.EntityType,
		"resource_type":  log.ResourceType,
		"resource_id":    log.ResourceID,
		"http_method":    log.HTTPMethod,
		"endpoint":       log.Endpoint,
		"status_code":    log.StatusCode,
		"outcome":        log.Outcome,
		"severity":       log.Severity,
		"error_detail":   log.ErrorDetail,
	}
	if len(log.ExtraMetadata) > 0 {
		for k, v := range log.ExtraMetadata {
			details[k] = v
		}
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("audit repository: marshalling details: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO audit_logs (id, organization_id, event_type, actor, details, ip_address, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		log.ID,
		log.OrganizationID,
		log.EventType,
		log.ActorClientID,
		detailsJSON,
		log.RequestIP,
		log.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("audit repository: insert: %w", err)
	}
	return nil
}

// QueryLogs retrieves audit logs matching the supplied filters.
func (r *AuditRepository) QueryLogs(ctx context.Context, filters AuditFilters) ([]AuditLog, error) {
	if filters.Limit <= 0 {
		filters.Limit = 100
	}

	var conditions []string
	args := []interface{}{}
	argN := 1

	if filters.OrganizationID != "" {
		conditions = append(conditions, fmt.Sprintf("organization_id = $%d", argN))
		args = append(args, filters.OrganizationID)
		argN++
	}
	if filters.EventType != "" {
		conditions = append(conditions, fmt.Sprintf("event_type = $%d", argN))
		args = append(args, filters.EventType)
		argN++
	}
	if filters.From != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argN))
		args = append(args, *filters.From)
		argN++
	}
	if filters.To != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argN))
		args = append(args, *filters.To)
		argN++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT id, organization_id, event_type, actor, details, ip_address, created_at
		FROM audit_logs
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argN, argN+1)

	args = append(args, filters.Limit, filters.Offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit repository: query logs: %w", err)
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var al AuditLog
		var detailsJSON []byte
		if err := rows.Scan(
			&al.ID,
			&al.OrganizationID,
			&al.EventType,
			&al.ActorClientID,
			&detailsJSON,
			&al.RequestIP,
			&al.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("audit repository: scanning row: %w", err)
		}
		// Unpack details back into fields.
		if len(detailsJSON) > 0 {
			var details map[string]interface{}
			if jsonErr := json.Unmarshal(detailsJSON, &details); jsonErr == nil {
				al.EventCategory, _ = details["event_category"].(string)
				al.Outcome, _ = details["outcome"].(string)
				al.Severity, _ = details["severity"].(string)
				al.ErrorDetail, _ = details["error_detail"].(string)
			}
		}
		logs = append(logs, al)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit repository: iterating rows: %w", err)
	}
	return logs, nil
}

// QueryStats returns counts of audit logs by outcome for the given organization.
func (r *AuditRepository) QueryStats(ctx context.Context, orgID string) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT details->>'outcome' AS outcome, COUNT(*) AS cnt
		FROM audit_logs
		WHERE organization_id = $1
		GROUP BY details->>'outcome'
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("audit repository: query stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var outcome string
		var cnt int
		if err := rows.Scan(&outcome, &cnt); err != nil {
			return nil, fmt.Errorf("audit repository: scanning stats row: %w", err)
		}
		stats[outcome] = cnt
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit repository: iterating stats rows: %w", err)
	}
	return stats, nil
}
