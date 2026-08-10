BEGIN;

CREATE TABLE room_inventory (
    hotel_id UUID NOT NULL,
    room_type_id UUID NOT NULL,
    inventory_date DATE NOT NULL,
    total_quantity INTEGER NOT NULL,
    held_quantity INTEGER NOT NULL DEFAULT 0,
    booked_quantity INTEGER NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT room_inventory_pkey
        PRIMARY KEY (hotel_id, room_type_id, inventory_date),
    CONSTRAINT room_inventory_total_quantity_nonnegative
        CHECK (total_quantity >= 0),
    CONSTRAINT room_inventory_held_quantity_nonnegative
        CHECK (held_quantity >= 0),
    CONSTRAINT room_inventory_booked_quantity_nonnegative
        CHECK (booked_quantity >= 0),
    CONSTRAINT room_inventory_capacity_not_exceeded
        CHECK (held_quantity + booked_quantity <= total_quantity),
    CONSTRAINT room_inventory_version_nonnegative
        CHECK (version >= 0)
);

CREATE TABLE reservations (
    id UUID PRIMARY KEY,
    booking_id UUID NOT NULL,
    hotel_id UUID NOT NULL,
    room_type_id UUID NOT NULL,
    check_in DATE NOT NULL,
    check_out DATE NOT NULL,
    quantity INTEGER NOT NULL,
    status VARCHAR(30) NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT reservations_booking_id_key UNIQUE (booking_id),
    CONSTRAINT reservations_valid_date_range CHECK (check_out > check_in),
    CONSTRAINT reservations_positive_quantity CHECK (quantity > 0),
    CONSTRAINT reservations_valid_status
        CHECK (status IN ('HELD', 'BOOKED', 'RELEASED', 'EXPIRED')),
    CONSTRAINT reservations_held_has_expiry
        CHECK (status <> 'HELD' OR expires_at IS NOT NULL)
);

CREATE INDEX reservations_held_expiry_idx
    ON reservations (expires_at, id)
    WHERE status = 'HELD';

CREATE TABLE reservation_inventory (
    reservation_id UUID NOT NULL,
    inventory_date DATE NOT NULL,
    quantity INTEGER NOT NULL,

    CONSTRAINT reservation_inventory_pkey
        PRIMARY KEY (reservation_id, inventory_date),
    CONSTRAINT reservation_inventory_reservation_fkey
        FOREIGN KEY (reservation_id)
        REFERENCES reservations (id)
        ON DELETE RESTRICT,
    CONSTRAINT reservation_inventory_positive_quantity
        CHECK (quantity > 0)
);

CREATE TABLE availability_outbox_events (
    id UUID PRIMARY KEY,
    aggregate_type VARCHAR(80) NOT NULL,
    aggregate_id UUID NOT NULL,
    aggregate_version BIGINT NOT NULL,
    event_type VARCHAR(120) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'PENDING',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    claimed_at TIMESTAMPTZ,
    claim_token UUID,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMPTZ,

    CONSTRAINT availability_outbox_aggregate_version_nonnegative
        CHECK (aggregate_version >= 0),
    CONSTRAINT availability_outbox_attempt_count_nonnegative
        CHECK (attempt_count >= 0),
    CONSTRAINT availability_outbox_valid_status
        CHECK (status IN ('PENDING', 'PUBLISHING', 'PUBLISHED', 'FAILED')),
    CONSTRAINT availability_outbox_published_consistency
        CHECK (
            (status = 'PUBLISHED' AND published_at IS NOT NULL)
            OR
            (status <> 'PUBLISHED' AND published_at IS NULL)
        )
);

CREATE INDEX availability_outbox_ready_idx
    ON availability_outbox_events (available_at, created_at, id)
    WHERE status IN ('PENDING', 'FAILED');

CREATE INDEX availability_outbox_aggregate_idx
    ON availability_outbox_events (
        aggregate_type,
        aggregate_id,
        aggregate_version
    );

COMMIT;
