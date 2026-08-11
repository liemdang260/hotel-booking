UPDATE booking_outbox_events SET status='PENDING',claim_token=NULL,claimed_at=NULL,locked_until=NULL
WHERE status='PUBLISHING';

DROP INDEX IF EXISTS booking_outbox_lease_idx;
ALTER TABLE booking_outbox_events
    DROP CONSTRAINT booking_outbox_events_claim_consistency,
    DROP CONSTRAINT booking_outbox_events_status_check,
    DROP COLUMN last_error,
    DROP COLUMN locked_until,
    DROP COLUMN claimed_at,
    DROP COLUMN claim_token,
    ADD CONSTRAINT booking_outbox_events_status_check
        CHECK (status IN ('PENDING','PUBLISHED','FAILED'));
