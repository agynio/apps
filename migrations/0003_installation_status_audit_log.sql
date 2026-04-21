-- 0003_installation_status_audit_log.sql

-- 1. Add status field for installations.
ALTER TABLE app_installations ADD COLUMN status TEXT;

-- 2. Create installation audit log entries table.
CREATE TABLE installation_audit_log_entries (
    id               UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    installation_id  UUID        NOT NULL REFERENCES app_installations (id) ON DELETE CASCADE,
    message          TEXT        NOT NULL,
    level            TEXT        NOT NULL,
    idempotency_key  TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX installation_audit_log_entries_installation_id_created_at_idx
    ON installation_audit_log_entries (installation_id, created_at DESC, id DESC);
CREATE INDEX installation_audit_log_entries_idempotency_idx
    ON installation_audit_log_entries (installation_id, idempotency_key);
