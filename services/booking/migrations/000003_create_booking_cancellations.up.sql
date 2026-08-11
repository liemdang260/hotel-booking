CREATE TABLE booking_cancellations (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 booking_id UUID NOT NULL REFERENCES bookings(id) ON DELETE RESTRICT,
 idempotency_key VARCHAR(255) NOT NULL UNIQUE,
 request_hash CHAR(64) NOT NULL,
 state VARCHAR(40) NOT NULL CHECK (state IN ('STARTED','POLICY_APPROVED','CANCELLING_RESERVATION','RESERVATION_CANCELLED','REFUND_PROCESSING','REFUND_UNKNOWN','COMPLETED','POLICY_REJECTED','FAILED')),
 reason VARCHAR(128) NOT NULL,
 policy_evaluated_at TIMESTAMPTZ NOT NULL,
 refund_amount_minor BIGINT NOT NULL CHECK (refund_amount_minor >= 0),
 currency CHAR(3) NOT NULL,
 refund_id VARCHAR(255),
 failure_code VARCHAR(128),
 retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
 next_retry_at TIMESTAMPTZ,
 version BIGINT NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 completed_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX booking_cancellations_one_active_per_booking
 ON booking_cancellations(booking_id)
 WHERE state NOT IN ('COMPLETED','POLICY_REJECTED','FAILED');
CREATE INDEX booking_cancellations_recovery
 ON booking_cancellations(next_retry_at,updated_at)
 WHERE state IN ('POLICY_APPROVED','CANCELLING_RESERVATION','RESERVATION_CANCELLED','REFUND_PROCESSING','REFUND_UNKNOWN');
