CREATE TABLE booking_cancellation_policies (
    booking_id uuid PRIMARY KEY REFERENCES bookings(id) ON DELETE CASCADE,
    policy_code text NOT NULL,
    policy_version text NOT NULL,
    free_cancel_until timestamptz NOT NULL,
    refund_basis_points integer NOT NULL CHECK (refund_basis_points BETWEEN 0 AND 10000),
    cancellation_fee_minor bigint NOT NULL CHECK (cancellation_fee_minor >= 0),
    currency char(3) NOT NULL,
    pricing_version text NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE OR REPLACE FUNCTION reject_booking_cancellation_policy_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'booking cancellation policy snapshots are immutable';
END;
$$;

CREATE TRIGGER booking_cancellation_policy_immutable
BEFORE UPDATE ON booking_cancellation_policies
FOR EACH ROW EXECUTE FUNCTION reject_booking_cancellation_policy_update();
