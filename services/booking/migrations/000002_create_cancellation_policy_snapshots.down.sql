DROP TRIGGER IF EXISTS booking_cancellation_policy_immutable ON booking_cancellation_policies;
DROP FUNCTION IF EXISTS reject_booking_cancellation_policy_update();
DROP TABLE IF EXISTS booking_cancellation_policies;
