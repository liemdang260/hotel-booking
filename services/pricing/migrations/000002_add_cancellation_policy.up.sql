ALTER TABLE quotes
    ADD COLUMN cancellation_policy_code text NOT NULL DEFAULT 'LEGACY',
    ADD COLUMN cancellation_policy_version text NOT NULL DEFAULT 'legacy-v1',
    ADD COLUMN free_cancel_until timestamptz NOT NULL DEFAULT '-infinity',
    ADD COLUMN refund_basis_points integer NOT NULL DEFAULT 0 CHECK (refund_basis_points BETWEEN 0 AND 10000),
    ADD COLUMN cancellation_fee_minor bigint NOT NULL DEFAULT 0 CHECK (cancellation_fee_minor >= 0);

ALTER TABLE quotes
    ALTER COLUMN cancellation_policy_code DROP DEFAULT,
    ALTER COLUMN cancellation_policy_version DROP DEFAULT,
    ALTER COLUMN free_cancel_until DROP DEFAULT,
    ALTER COLUMN refund_basis_points DROP DEFAULT,
    ALTER COLUMN cancellation_fee_minor DROP DEFAULT;
