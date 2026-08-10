BEGIN;

DROP TABLE availability_outbox_events;
DROP TABLE reservation_inventory;
DROP TABLE reservations;
DROP TABLE room_inventory;

COMMIT;
