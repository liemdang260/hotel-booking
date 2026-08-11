BEGIN;

CREATE TABLE notification_processed_events (
    event_id UUID PRIMARY KEY,
    event_type VARCHAR(120) NOT NULL,
    event_version INTEGER NOT NULL CHECK (event_version > 0),
    processed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE notification_jobs (
    event_id UUID PRIMARY KEY REFERENCES notification_processed_events(event_id) ON DELETE RESTRICT,
    kind VARCHAR(80) NOT NULL,
    payload BYTEA NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING','SENDING','SENT','FAILED')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    sent_at TIMESTAMPTZ
);

CREATE INDEX notification_jobs_pending_idx
    ON notification_jobs (next_attempt_at, created_at)
    WHERE status IN ('PENDING','FAILED');

COMMIT;
