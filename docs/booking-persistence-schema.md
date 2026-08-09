# Booking Persistence Schema

## Purpose

This document defines Booking Service persistence around the Clean Architecture and Saga boundaries described in `booking-service-design.md`.

The database is owned exclusively by Booking Service. Other services must not query these tables directly.

## Core tables

Initial tables:

```text
bookings
booking_price_snapshots
booking_price_items
booking_sagas
booking_idempotency
outbox_events
```

## Bookings

```sql
CREATE TABLE bookings (
    id UUID PRIMARY KEY,

    user_id UUID NOT NULL,
    hotel_id UUID NOT NULL,
    room_type_id UUID NOT NULL,

    check_in DATE NOT NULL,
    check_out DATE NOT NULL,

    guest_count INTEGER NOT NULL,
    room_quantity INTEGER NOT NULL,

    status VARCHAR(32) NOT NULL,

    reservation_id UUID,
    payment_id UUID,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (check_out > check_in),
    CHECK (guest_count > 0),
    CHECK (room_quantity > 0)
);
```

Booking Service stores external identifiers such as `reservation_id`, `payment_id`, `hotel_id`, and `room_type_id` as opaque references. There are no cross-service foreign keys.

## Booking status

Initial application-level states:

```text
PENDING
INVENTORY_RESERVED
PAYMENT_PROCESSING
PAYMENT_UNKNOWN
PAYMENT_FAILED
CONFIRMED
CANCELLED
EXPIRED
FAILED
```

The database stores the status as a string with application validation rather than a PostgreSQL enum so state evolution does not require enum-alter migrations.

## Booking price snapshot

Price must be immutable for an accepted booking attempt.

```sql
CREATE TABLE booking_price_snapshots (
    booking_id UUID PRIMARY KEY
        REFERENCES bookings(id) ON DELETE CASCADE,

    currency VARCHAR(3) NOT NULL,

    subtotal BIGINT NOT NULL,
    tax BIGINT NOT NULL,
    service_fee BIGINT NOT NULL,
    discount BIGINT NOT NULL DEFAULT 0,
    total_amount BIGINT NOT NULL,

    pricing_version VARCHAR(100),
    quoted_at TIMESTAMPTZ NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (subtotal >= 0),
    CHECK (tax >= 0),
    CHECK (service_fee >= 0),
    CHECK (discount >= 0),
    CHECK (total_amount >= 0)
);
```

Money is stored in the smallest currency unit. For example USD 340.50 is persisted as `34050`.

Floating-point types must not be used for monetary values.

## Price items

Optional breakdown used for invoices/debugging:

```sql
CREATE TABLE booking_price_items (
    id UUID PRIMARY KEY,
    booking_id UUID NOT NULL
        REFERENCES bookings(id) ON DELETE CASCADE,

    item_type VARCHAR(50) NOT NULL,
    description VARCHAR(255),
    amount BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Examples:

```text
ROOM_RATE       30000
TAX              3000
SERVICE_FEE      1000
PROMOTION       -2000
```

The `booking_price_snapshots.total_amount` remains authoritative. Price items explain how it was composed.

## Saga persistence

```sql
CREATE TABLE booking_sagas (
    id UUID PRIMARY KEY,
    booking_id UUID NOT NULL UNIQUE
        REFERENCES bookings(id) ON DELETE CASCADE,

    state VARCHAR(50) NOT NULL,

    reservation_id UUID,
    payment_id UUID,

    last_error_code VARCHAR(100),
    last_error_message TEXT,

    retry_count INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,

    version BIGINT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (retry_count >= 0)
);
```

Suggested states:

```text
STARTED
PRICE_QUOTED
INVENTORY_RESERVED
PAYMENT_PROCESSING
PAYMENT_SUCCEEDED
PAYMENT_UNKNOWN
CONFIRMING_RESERVATION
COMPENSATING
COMPENSATED
COMPLETED
FAILED
```

The Saga table is execution/recovery state. It is not a replacement for the Booking aggregate's customer-facing status.

## Why Booking and Saga state are separate

Example:

```text
Booking status: PAYMENT_UNKNOWN
Saga state:     PAYMENT_UNKNOWN
```

Later during compensation:

```text
Booking status: PAYMENT_FAILED
Saga state:     COMPENSATING
```

The customer-facing booking lifecycle and internal workflow execution state evolve for different reasons, so they should not be represented by one overloaded status field.

## Idempotency

```sql
CREATE TABLE booking_idempotency (
    id UUID PRIMARY KEY,

    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    request_hash VARCHAR(128) NOT NULL,

    booking_id UUID
        REFERENCES bookings(id),

    status VARCHAR(32) NOT NULL,

    response_payload JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);
```

Possible statuses:

```text
PROCESSING
COMPLETED
FAILED
```

Rules:

1. Insert the idempotency key before allowing duplicate booking creation.
2. A unique constraint is the final concurrency defense.
3. Same key + same request hash resumes/returns the existing operation.
4. Same key + different request hash is an idempotency conflict.

## Request hash

The hash should cover fields that define the logical CreateBooking operation:

```text
user_id
hotel_id
room_type_id
check_in
check_out
guest_count
room_quantity
payment_method reference when semantics require it
```

Canonical serialization must be deterministic before hashing.

## Outbox

```sql
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,

    aggregate_type VARCHAR(50) NOT NULL,
    aggregate_id UUID NOT NULL,

    event_type VARCHAR(100) NOT NULL,
    event_version INTEGER NOT NULL DEFAULT 1,

    payload JSONB NOT NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    retry_count INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,

    CHECK (retry_count >= 0)
);
```

The booking mutation and its event are committed in the same local transaction.

Example:

```text
BEGIN
  booking -> CONFIRMED
  saga -> COMPLETED
  insert BookingConfirmed outbox event
COMMIT
```

## Indexes

```sql
CREATE INDEX idx_bookings_user_created
ON bookings(user_id, created_at DESC);

CREATE INDEX idx_bookings_status_updated
ON bookings(status, updated_at);

CREATE INDEX idx_booking_sagas_recovery
ON booking_sagas(next_retry_at, id)
WHERE state IN ('PAYMENT_UNKNOWN', 'COMPENSATING', 'FAILED');

CREATE INDEX idx_outbox_pending
ON outbox_events(next_retry_at, created_at)
WHERE status = 'PENDING';
```

`booking_idempotency.idempotency_key` and `booking_sagas.booking_id` already receive indexes through their unique constraints and do not need duplicate indexes.

## CreateBooking transaction 1: idempotency and local creation

A key design point is that remote gRPC calls must not execute while holding a Booking DB transaction open.

Initial flow:

```text
PricingRepository.Quote
       |
       v
BEGIN Booking DB transaction
  claim idempotency key
  create booking(PENDING)
  save immutable price snapshot
  create saga(PRICE_QUOTED)
COMMIT
```

There is an alternative to claim idempotency before Pricing if pricing calls are expensive. The implementation may insert a lightweight PROCESSING idempotency row first, then call Pricing outside the transaction and create the booking in a second local transaction. The chosen implementation must still guarantee one logical booking per idempotency key.

## Reservation persistence transaction

After `AvailabilityRepository.ReserveInventory` succeeds:

```text
BEGIN
  lock booking/saga if needed
  booking.reservation_id = result.id
  booking.status = INVENTORY_RESERVED
  saga.reservation_id = result.id
  saga.state = INVENTORY_RESERVED
COMMIT
```

The reservation remote side effect happened before this transaction. If Booking Service crashes before persistence, recovery can call Availability using `booking_id`, because ReserveInventory is idempotent and Availability can return the existing reservation.

## Payment-processing transaction

Before calling Payment:

```text
BEGIN
  booking.status = PAYMENT_PROCESSING
  saga.state = PAYMENT_PROCESSING
COMMIT
```

Then call Payment outside the DB transaction.

This durable marker shows that the Saga had advanced to payment even if the process dies during the remote call.

## Payment success persistence

After an authoritative success result:

```text
BEGIN
  booking.payment_id = result.payment_id
  saga.payment_id = result.payment_id
  saga.state = PAYMENT_SUCCEEDED
COMMIT
```

Then call `ConfirmReservation` outside the transaction.

## Payment failure

After an authoritative failure:

```text
BEGIN
  booking.status = PAYMENT_FAILED
  saga.state = COMPENSATING
  persist error details
COMMIT
```

Then execute `AvailabilityRepository.ReleaseReservation`.

After successful compensation:

```text
BEGIN
  saga.state = COMPENSATED
  insert BookingFailed/BookingExpired outbox event if required
  save idempotency final response
COMMIT
```

## Payment unknown

When Payment returns a transport timeout/ambiguous outcome:

```text
BEGIN
  booking.status = PAYMENT_UNKNOWN
  saga.state = PAYMENT_UNKNOWN
  saga.retry_count += 1
  saga.next_retry_at = calculated backoff time
  persist last error
COMMIT
```

No inventory release is triggered at this point.

The Saga recovery worker later asks Payment Service for authoritative payment state.

## Confirmation transaction

After Payment succeeded and Availability confirms the reservation:

```text
BEGIN
  booking.status = CONFIRMED
  saga.state = COMPLETED
  insert BookingConfirmed into outbox_events
  mark booking_idempotency COMPLETED
  save response_payload
COMMIT
```

This gives the invariant:

> A committed CONFIRMED booking has a durable BookingConfirmed event waiting in the outbox.

## Confirm failure after successful payment

If Availability returns `RESERVATION_EXPIRED` after Payment succeeded:

```text
BEGIN
  saga.state = COMPENSATING
  persist failure reason
COMMIT
```

Then:

```text
PaymentRepository.RefundPayment
```

After successful refund:

```text
BEGIN
  booking.status = FAILED
  saga.state = COMPENSATED
  insert BookingFailed event
  finalize idempotency result
COMMIT
```

The refund call must not run inside the Booking DB transaction.

## Saga recovery worker query

Workers may claim recoverable Saga rows using short transactions and `SKIP LOCKED`.

Conceptual query:

```sql
SELECT id, booking_id, state
FROM booking_sagas
WHERE next_retry_at IS NOT NULL
  AND next_retry_at <= NOW()
  AND state IN ('PAYMENT_UNKNOWN', 'COMPENSATING')
ORDER BY next_retry_at
LIMIT 1
FOR UPDATE SKIP LOCKED;
```

The implementation should avoid holding this row lock across remote calls. One strategy is to atomically claim the Saga by moving `next_retry_at` forward or storing a lease/processing marker, commit, perform remote reconciliation, then persist the result in a new transaction.

## Optimistic version

`booking_sagas.version` allows later optimistic concurrency control if multiple recovery paths can update the same Saga.

Example:

```sql
UPDATE booking_sagas
SET
    state = $new_state,
    version = version + 1,
    updated_at = NOW()
WHERE id = $id
  AND version = $expected_version;
```

Zero rows affected means another worker/process already changed the Saga.

The first implementation may still use row locks for local mutations, but preserving `version` leaves a clean optimization path.

## Repository-oriented persistence API

The usecase should work with business operations rather than raw SQL table concepts.

Conceptual interfaces:

```go
type BookingRepository interface {
    FindByID(ctx context.Context, bookingID string) (*entity.Booking, error)
    FindByIdempotencyKey(ctx context.Context, key string) (*entity.Booking, error)

    WithTx(
        ctx context.Context,
        fn func(TxBookingRepository) error,
    ) error
}
```

```go
type TxBookingRepository interface {
    CreateBooking(ctx context.Context, booking *entity.Booking) error
    SaveBooking(ctx context.Context, booking *entity.Booking) error

    CreateSaga(ctx context.Context, saga *entity.BookingSaga) error
    SaveSaga(ctx context.Context, saga *entity.BookingSaga) error

    ClaimIdempotencyKey(ctx context.Context, record IdempotencyRecord) error
    CompleteIdempotency(ctx context.Context, key string, response any) error

    SavePriceSnapshot(ctx context.Context, snapshot entity.PriceSnapshot) error
    InsertOutboxEvent(ctx context.Context, event entity.OutboxEvent) error
}
```

The PostgreSQL implementation decides the SQL details. The usecase decides when these methods must be atomic together.

## Persistence invariants

Booking Service must guarantee:

1. One idempotency key maps to at most one logical CreateBooking operation.
2. A CONFIRMED booking has a persisted payment reference and reservation reference.
3. A CONFIRMED booking and BookingConfirmed outbox event are committed atomically.
4. Remote calls never occur inside long-lived Booking DB transactions.
5. Saga progress is persisted between remote side effects.
6. PAYMENT_UNKNOWN does not trigger automatic inventory release.
7. External identifiers are opaque references, never cross-service foreign keys.
8. Booking price is an immutable snapshot once the booking attempt is accepted.

## Next step

Define Booking's transport contract and concrete adapter contracts:

- external REST API exposed through API Gateway
- internal Booking gRPC API if needed
- PricingRepository gRPC adapter contract
- AvailabilityRepository gRPC adapter contract
- PaymentRepository gRPC adapter contract
- error translation and retry/deadline policy