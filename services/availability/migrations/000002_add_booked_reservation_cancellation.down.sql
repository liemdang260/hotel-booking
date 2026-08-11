BEGIN;

ALTER TABLE reservations
    DROP CONSTRAINT reservations_valid_status;

ALTER TABLE reservations
    ADD CONSTRAINT reservations_valid_status
        CHECK (status IN ('HELD', 'BOOKED', 'RELEASED', 'EXPIRED'));

COMMIT;
