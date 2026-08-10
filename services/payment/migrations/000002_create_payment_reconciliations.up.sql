CREATE TABLE payment_reconciliations (
    payment_id text PRIMARY KEY REFERENCES payments(id),
    status text NOT NULL CHECK (status IN ('PENDING','CLAIMED','RESOLVED','EXHAUSTED')),
    retry_count integer NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    max_attempts integer NOT NULL CHECK (max_attempts > 0),
    next_retry_at timestamptz NOT NULL,
    lease_until timestamptz,
    last_error_code text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE INDEX payment_reconciliations_due_idx
    ON payment_reconciliations (next_retry_at, payment_id)
    WHERE status = 'PENDING';
