-- Migration 003: failed_calls table.
-- Records exhausted retry failures for investigation.
-- Idempotent: uses IF NOT EXISTS throughout.

CREATE TABLE IF NOT EXISTS failed_calls (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id          UUID        REFERENCES jobs(id) ON DELETE SET NULL,
    service         TEXT        NOT NULL,
    method          TEXT        NOT NULL,
    url             TEXT        NOT NULL,
    request_body    TEXT,
    response_status INTEGER,
    response_body   TEXT,
    error_message   TEXT,
    attempts        INTEGER     NOT NULL DEFAULT 1,
    first_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_failed_calls_job_id     ON failed_calls (job_id);
CREATE INDEX IF NOT EXISTS idx_failed_calls_service    ON failed_calls (service);
CREATE INDEX IF NOT EXISTS idx_failed_calls_created_at ON failed_calls (created_at DESC);
