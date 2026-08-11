ALTER TABLE quotes
    DROP COLUMN cancellation_fee_minor,
    DROP COLUMN refund_basis_points,
    DROP COLUMN free_cancel_until,
    DROP COLUMN cancellation_policy_version,
    DROP COLUMN cancellation_policy_code;
