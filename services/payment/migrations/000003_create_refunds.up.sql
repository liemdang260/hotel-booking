CREATE TABLE refunds (
    id text PRIMARY KEY,
    payment_id text NOT NULL REFERENCES payments(id),
    booking_id text NOT NULL,
    idempotency_key text NOT NULL,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency varchar(3) NOT NULL CHECK (currency = upper(currency)),
    status text NOT NULL CHECK (status IN ('PENDING','PROCESSING','SUCCEEDED','FAILED','UNKNOWN')),
    provider_reference text NOT NULL DEFAULT '',
    failure_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT refunds_idempotency_key_key UNIQUE (idempotency_key)
);

CREATE TABLE refund_attempts (
    id text PRIMARY KEY,
    refund_id text NOT NULL REFERENCES refunds(id),
    outcome text NOT NULL CHECK (outcome IN ('STARTED','SUCCEEDED','DECLINED','UNKNOWN')),
    provider_request_ref text NOT NULL DEFAULT '',
    provider_reference text NOT NULL DEFAULT '',
    failure_code text NOT NULL DEFAULT '',
    raw_outcome text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL,
    finished_at timestamptz
);

CREATE INDEX refund_attempts_refund_started_idx
    ON refund_attempts (refund_id, started_at DESC);
