ALTER TABLE booking_outbox_events
    DROP CONSTRAINT booking_outbox_events_status_check,
    ADD COLUMN claim_token UUID,
    ADD COLUMN claimed_at TIMESTAMPTZ,
    ADD COLUMN locked_until TIMESTAMPTZ,
    ADD COLUMN last_error TEXT,
    ADD CONSTRAINT booking_outbox_events_status_check
        CHECK (status IN ('PENDING','PUBLISHING','PUBLISHED','FAILED')),
    ADD CONSTRAINT booking_outbox_events_claim_consistency
        CHECK (
            (status = 'PUBLISHING' AND claim_token IS NOT NULL AND claimed_at IS NOT NULL AND locked_until IS NOT NULL)
            OR
            (status <> 'PUBLISHING' AND claim_token IS NULL AND claimed_at IS NULL AND locked_until IS NULL)
        );

CREATE INDEX booking_outbox_lease_idx ON booking_outbox_events (locked_until)
    WHERE status = 'PUBLISHING';
