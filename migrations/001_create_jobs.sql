-- Migration 001: jobs and job_checkpoints tables.
-- Idempotent: uses IF NOT EXISTS throughout.

CREATE TABLE IF NOT EXISTS jobs (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id             TEXT        NOT NULL,
    docusign_account_id         TEXT        NOT NULL,
    status                      TEXT        NOT NULL DEFAULT 'PENDING',
    stage                       TEXT,
    date_range_start            TIMESTAMPTZ NOT NULL,
    date_range_end              TIMESTAMPTZ NOT NULL,
    envelopes_processed         INTEGER     NOT NULL DEFAULT 0,
    recipients_processed        INTEGER     NOT NULL DEFAULT 0,
    documents_processed         INTEGER     NOT NULL DEFAULT 0,
    templates_processed         INTEGER     NOT NULL DEFAULT 0,
    started_at                  TIMESTAMPTZ,
    completed_at                TIMESTAMPTZ,
    last_heartbeat_at           TIMESTAMPTZ,
    error_message               TEXT,
    error_code                  TEXT,
    cancelled_at                TIMESTAMPTZ,
    cancellation_reason         TEXT,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_jobs_organization_id   ON jobs (organization_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status            ON jobs (status);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at        ON jobs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_last_heartbeat_at ON jobs (last_heartbeat_at);

CREATE TABLE IF NOT EXISTS job_checkpoints (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id          UUID        NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    checkpoint_type TEXT        NOT NULL,
    page_position   TEXT,
    last_processed  TEXT,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id, checkpoint_type)
);

CREATE INDEX IF NOT EXISTS idx_job_checkpoints_job_id ON job_checkpoints (job_id);
