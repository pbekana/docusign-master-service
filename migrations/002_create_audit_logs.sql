-- Migration 002: audit_logs table.
-- Idempotent: uses IF NOT EXISTS throughout.

CREATE TABLE IF NOT EXISTS audit_logs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id          UUID        REFERENCES jobs(id) ON DELETE SET NULL,
    organization_id TEXT,
    event_type      TEXT        NOT NULL,
    actor           TEXT,
    details         JSONB,
    ip_address      TEXT,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_job_id          ON audit_logs (job_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_organization_id  ON audit_logs (organization_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_event_type       ON audit_logs (event_type);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at       ON audit_logs (created_at DESC);
