ALTER TABLE availability_outbox_events
    ADD COLUMN locked_until TIMESTAMPTZ,
    ADD CONSTRAINT availability_outbox_claim_consistency
        CHECK (
            (status = 'PUBLISHING' AND claim_token IS NOT NULL AND claimed_at IS NOT NULL AND locked_until IS NOT NULL)
            OR
            (status <> 'PUBLISHING' AND claim_token IS NULL AND claimed_at IS NULL AND locked_until IS NULL)
        );

CREATE INDEX availability_outbox_lease_idx ON availability_outbox_events (locked_until)
    WHERE status = 'PUBLISHING';
