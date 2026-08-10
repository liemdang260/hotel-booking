CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE bookings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(255) NOT NULL,
    hotel_id VARCHAR(255) NOT NULL,
    room_type_id VARCHAR(255) NOT NULL,
    check_in DATE NOT NULL,
    check_out DATE NOT NULL,
    guest_count INTEGER NOT NULL CHECK (guest_count > 0),
    room_quantity INTEGER NOT NULL CHECK (room_quantity > 0),
    status VARCHAR(32) NOT NULL CHECK (status IN (
        'PENDING','INVENTORY_RESERVED','PAYMENT_PROCESSING','PAYMENT_UNKNOWN',
        'PAYMENT_FAILED','CONFIRMED','CANCELLED','FAILED'
    )),
    reservation_id VARCHAR(255),
    payment_id VARCHAR(255),
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (check_out > check_in)
);

CREATE TABLE booking_price_snapshots (
    booking_id UUID PRIMARY KEY REFERENCES bookings(id) ON DELETE RESTRICT,
    quote_id VARCHAR(255) NOT NULL UNIQUE,
    currency CHAR(3) NOT NULL,
    subtotal_minor BIGINT NOT NULL CHECK (subtotal_minor >= 0),
    tax_minor BIGINT NOT NULL CHECK (tax_minor >= 0),
    service_fee_minor BIGINT NOT NULL CHECK (service_fee_minor >= 0),
    discount_minor BIGINT NOT NULL CHECK (discount_minor >= 0),
    total_minor BIGINT NOT NULL CHECK (total_minor >= 0),
    pricing_version VARCHAR(64) NOT NULL,
    quoted_at TIMESTAMPTZ NOT NULL,
    quote_expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (total_minor = subtotal_minor + tax_minor + service_fee_minor - discount_minor),
    CHECK (quote_expires_at > quoted_at),
    CHECK (accepted_at <= quote_expires_at)
);

CREATE TABLE booking_sagas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id UUID NOT NULL UNIQUE REFERENCES bookings(id) ON DELETE RESTRICT,
    state VARCHAR(40) NOT NULL CHECK (state IN (
        'PRICE_ACCEPTED','RESERVING_INVENTORY','INVENTORY_RESERVED',
        'PAYMENT_PROCESSING','PAYMENT_UNKNOWN','CONFIRMING_RESERVATION',
        'COMPLETED','COMPENSATING','COMPENSATED','FAILED'
    )),
    reservation_id VARCHAR(255),
    payment_id VARCHAR(255),
    last_error_code VARCHAR(128),
    last_error_message TEXT,
    retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    next_retry_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE booking_idempotency (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    request_hash CHAR(64) NOT NULL,
    booking_id UUID REFERENCES bookings(id) ON DELETE RESTRICT,
    status VARCHAR(16) NOT NULL CHECK (status IN ('PROCESSING','COMPLETED','FAILED')),
    response_payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE booking_outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_type VARCHAR(64) NOT NULL,
    aggregate_id UUID NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    event_version INTEGER NOT NULL CHECK (event_version > 0),
    payload JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING','PUBLISHED','FAILED')),
    retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

CREATE INDEX bookings_user_created_idx ON bookings (user_id, created_at DESC);
CREATE INDEX bookings_status_updated_idx ON bookings (status, updated_at);
CREATE INDEX booking_sagas_recovery_idx ON booking_sagas (next_retry_at)
    WHERE state IN ('PAYMENT_UNKNOWN','CONFIRMING_RESERVATION','COMPENSATING');
CREATE INDEX booking_idempotency_expiry_idx ON booking_idempotency (expires_at);
CREATE INDEX booking_outbox_pending_idx ON booking_outbox_events (next_retry_at, created_at)
    WHERE status IN ('PENDING','FAILED');

COMMENT ON COLUMN booking_price_snapshots.quote_id IS
    'Opaque Pricing-service identifier; the accepted quote is immutable after insertion.';
COMMENT ON COLUMN bookings.status IS
    'Customer-visible booking lifecycle; intentionally separate from booking_sagas.state.';
