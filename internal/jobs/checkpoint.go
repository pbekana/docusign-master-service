package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckpointRepository persists and retrieves job progress checkpoints.
type CheckpointRepository struct {
	pool *pgxpool.Pool
}

// NewCheckpointRepository creates a new CheckpointRepository.
func NewCheckpointRepository(pool *pgxpool.Pool) *CheckpointRepository {
	return &CheckpointRepository{pool: pool}
}

// Upsert inserts or updates a checkpoint record. Conflict is keyed on (job_id, checkpoint_type).
func (r *CheckpointRepository) Upsert(ctx context.Context, cp JobCheckpoint) error {
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CheckpointAt.IsZero() {
		cp.CheckpointAt = time.Now().UTC()
	}

	// Encode extra fields into metadata jsonb.
	meta := map[string]interface{}{
		"cursor_position":   cp.CursorPosition,
		"records_processed": cp.RecordsProcessed,
		"last_envelope_id":  cp.LastEnvelopeID,
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("checkpoint repository: marshalling metadata: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO job_checkpoints (id, job_id, checkpoint_type, page_position, last_processed, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (job_id, checkpoint_type)
		DO UPDATE SET
			page_position  = EXCLUDED.page_position,
			last_processed = EXCLUDED.last_processed,
			metadata       = EXCLUDED.metadata,
			updated_at     = EXCLUDED.updated_at
	`,
		cp.ID,
		cp.JobID,
		cp.Stage,
		fmt.Sprintf("%d", cp.CursorPosition),
		cp.LastEnvelopeID,
		metaJSON,
		cp.CheckpointAt,
	)
	if err != nil {
		return fmt.Errorf("checkpoint repository: upsert: %w", err)
	}
	return nil
}

// Get retrieves the checkpoint for a job at a given stage.
// Returns nil if no checkpoint exists (not an error).
func (r *CheckpointRepository) Get(ctx context.Context, jobID uuid.UUID, stage string) (*JobCheckpoint, error) {
	var cp JobCheckpoint
	var pagePos string
	var metaJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, job_id, checkpoint_type, COALESCE(page_position,'0'), COALESCE(last_processed,''), metadata, updated_at
		FROM job_checkpoints
		WHERE job_id=$1 AND checkpoint_type=$2
	`, jobID, stage).Scan(
		&cp.ID,
		&cp.JobID,
		&cp.Stage,
		&pagePos,
		&cp.LastEnvelopeID,
		&metaJSON,
		&cp.CheckpointAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("checkpoint repository: get: %w", err)
	}

	// Parse cursor from page_position.
	var cursor int
	fmt.Sscanf(pagePos, "%d", &cursor)
	cp.CursorPosition = cursor

	// Restore additional fields from metadata.
	if len(metaJSON) > 0 {
		var meta map[string]interface{}
		if jsonErr := json.Unmarshal(metaJSON, &meta); jsonErr == nil {
			if v, ok := meta["records_processed"].(float64); ok {
				cp.RecordsProcessed = int(v)
			}
		}
	}

	return &cp, nil
}

// Delete removes all checkpoints for the given job.
func (r *CheckpointRepository) Delete(ctx context.Context, jobID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM job_checkpoints WHERE job_id=$1`, jobID)
	if err != nil {
		return fmt.Errorf("checkpoint repository: delete: %w", err)
	}
	return nil
}
