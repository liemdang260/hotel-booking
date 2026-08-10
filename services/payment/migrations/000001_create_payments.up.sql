CREATE TABLE payments (
    id text PRIMARY KEY,
    booking_id text NOT NULL,
    idempotency_key text NOT NULL,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency varchar(3) NOT NULL CHECK (currency = upper(currency)),
    payment_method_ref text NOT NULL,
    status text NOT NULL CHECK (status IN ('PENDING','PROCESSING','SUCCEEDED','FAILED','UNKNOWN')),
    provider_reference text NOT NULL DEFAULT '',
    failure_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT payments_booking_id_key UNIQUE (booking_id),
    CONSTRAINT payments_idempotency_key_key UNIQUE (idempotency_key)
);

CREATE TABLE payment_attempts (
    id text PRIMARY KEY,
    payment_id text NOT NULL REFERENCES payments(id),
    idempotency_key text NOT NULL,
    provider_request_ref text NOT NULL DEFAULT '',
    provider_reference text NOT NULL DEFAULT '',
    outcome text NOT NULL CHECK (outcome IN ('STARTED','SUCCEEDED','DECLINED','UNKNOWN')),
    failure_code text NOT NULL DEFAULT '',
    raw_outcome text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    CONSTRAINT payment_attempts_payment_id_idempotency_key UNIQUE (payment_id, idempotency_key)
);

CREATE INDEX payment_attempts_payment_started_idx
    ON payment_attempts (payment_id, started_at DESC);
