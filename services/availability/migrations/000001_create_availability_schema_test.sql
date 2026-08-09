BEGIN;

DO $$
BEGIN
    IF to_regclass('public.room_inventory') IS NULL
        OR to_regclass('public.reservations') IS NULL
        OR to_regclass('public.reservation_inventory') IS NULL
        OR to_regclass('public.availability_outbox_events') IS NULL THEN
        RAISE EXCEPTION 'Availability schema tables are missing';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'room_inventory'
          AND column_name = 'inventory_date'
          AND data_type = 'date'
    ) THEN
        RAISE EXCEPTION 'room_inventory.inventory_date must use DATE';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'reservations'
          AND column_name = 'expires_at'
          AND data_type = 'timestamp with time zone'
    ) THEN
        RAISE EXCEPTION 'reservations.expires_at must use TIMESTAMPTZ';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_index i
        JOIN pg_class c ON c.oid = i.indexrelid
        WHERE c.relname = 'reservations_held_expiry_idx'
          AND i.indpred IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'HELD reservation expiry index must be partial';
    END IF;
END
$$;

INSERT INTO room_inventory (
    hotel_id,
    room_type_id,
    inventory_date,
    total_quantity,
    held_quantity,
    booked_quantity
) VALUES (
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000002',
    DATE '2026-09-01',
    10,
    4,
    3
);

DO $$
BEGIN
    BEGIN
        INSERT INTO room_inventory (
            hotel_id,
            room_type_id,
            inventory_date,
            total_quantity,
            held_quantity,
            booked_quantity
        ) VALUES (
            '00000000-0000-0000-0000-000000000001',
            '00000000-0000-0000-0000-000000000002',
            DATE '2026-09-02',
            10,
            6,
            5
        );
        RAISE EXCEPTION 'capacity constraint accepted oversold inventory';
    EXCEPTION
        WHEN check_violation THEN NULL;
    END;
END
$$;

INSERT INTO reservations (
    id,
    booking_id,
    hotel_id,
    room_type_id,
    check_in,
    check_out,
    quantity,
    status,
    expires_at
) VALUES
(
    '00000000-0000-0000-0000-000000000010',
    '00000000-0000-0000-0000-000000000011',
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000002',
    DATE '2026-09-01',
    DATE '2026-09-02',
    1,
    'HELD',
    TIMESTAMPTZ '2026-08-31 12:00:00+00'
),
(
    '00000000-0000-0000-0000-000000000020',
    '00000000-0000-0000-0000-000000000021',
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000002',
    DATE '2026-09-01',
    DATE '2026-09-02',
    1,
    'BOOKED',
    NULL
),
(
    '00000000-0000-0000-0000-000000000030',
    '00000000-0000-0000-0000-000000000031',
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000002',
    DATE '2026-09-01',
    DATE '2026-09-02',
    1,
    'RELEASED',
    NULL
),
(
    '00000000-0000-0000-0000-000000000040',
    '00000000-0000-0000-0000-000000000041',
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000002',
    DATE '2026-09-01',
    DATE '2026-09-02',
    1,
    'EXPIRED',
    NULL
);

DO $$
BEGIN
    BEGIN
        INSERT INTO reservations (
            id,
            booking_id,
            hotel_id,
            room_type_id,
            check_in,
            check_out,
            quantity,
            status,
            expires_at
        ) VALUES (
            '00000000-0000-0000-0000-000000000012',
            '00000000-0000-0000-0000-000000000011',
            '00000000-0000-0000-0000-000000000001',
            '00000000-0000-0000-0000-000000000002',
            DATE '2026-09-01',
            DATE '2026-09-02',
            1,
            'HELD',
            TIMESTAMPTZ '2026-08-31 12:00:00+00'
        );
        RAISE EXCEPTION 'duplicate booking_id was accepted';
    EXCEPTION
        WHEN unique_violation THEN NULL;
    END;
END
$$;

INSERT INTO reservation_inventory (
    reservation_id,
    inventory_date,
    quantity
) VALUES (
    '00000000-0000-0000-0000-000000000010',
    DATE '2026-09-01',
    1
);

INSERT INTO availability_outbox_events (
    id,
    aggregate_type,
    aggregate_id,
    aggregate_version,
    event_type,
    payload
) VALUES (
    '00000000-0000-0000-0000-000000000050',
    'reservation',
    '00000000-0000-0000-0000-000000000010',
    1,
    'ReservationHeld',
    '{"reservation_id":"00000000-0000-0000-0000-000000000010"}'::jsonb
);

DO $$
BEGIN
    BEGIN
        INSERT INTO availability_outbox_events (
            id,
            aggregate_type,
            aggregate_id,
            aggregate_version,
            event_type,
            payload,
            status,
            published_at
        ) VALUES (
            '00000000-0000-0000-0000-000000000051',
            'reservation',
            '00000000-0000-0000-0000-000000000010',
            2,
            'InvalidEvent',
            '{}'::jsonb,
            'PUBLISHED',
            NULL
        );
        RAISE EXCEPTION 'published outbox event without published_at was accepted';
    EXCEPTION
        WHEN check_violation THEN NULL;
    END;
END
$$;

ROLLBACK;
