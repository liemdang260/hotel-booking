UPDATE availability_outbox_events SET status='PENDING',claim_token=NULL,claimed_at=NULL,locked_until=NULL
WHERE status='PUBLISHING';

DROP INDEX IF EXISTS availability_outbox_lease_idx;
ALTER TABLE availability_outbox_events
    DROP CONSTRAINT availability_outbox_claim_consistency,
    DROP COLUMN locked_until;
