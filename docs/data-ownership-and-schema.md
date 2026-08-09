# Data Ownership and Database Schema

## Purpose

This document defines data ownership boundaries and the initial database design for the Hotel Booking Platform.

The goal is to make service boundaries follow business invariants rather than entity names, and to avoid coupling microservices through shared tables or cross-service foreign keys.

## Core principle

Each service owns its data. Other services access that data only through a service contract such as gRPC or through asynchronous events.

```text
Booking Service      -> booking_db
Availability Service -> availability_db
Pricing Service      -> pricing_db
Payment Service      -> payment_db
Auth Service         -> auth_db
Catalog Service      -> catalog_db
Notification Service -> notification_db
```

For local development, these databases may live on the same PostgreSQL instance, but they remain logically isolated so they can be separated later without changing business boundaries.

There must be no cross-service foreign keys or direct cross-service SQL queries.

---

# 1. Booking Service

Booking Service owns the booking aggregate, price snapshot, Saga progress, external request idempotency, and its own outbox events.

## bookings

```sql
CREATE TABLE bookings (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    hotel_id UUID NOT NULL,
    room_type_id UUID NOT NULL,
    check_in DATE NOT NULL,
    check_out DATE NOT NULL,
    guest_count INT NOT NULL,
    room_quantity INT NOT NULL,
    status VARCHAR(50) NOT NULL,
    reservation_id UUID,
    payment_id UUID,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

Suggested statuses:

```text
PENDING
INVENTORY_RESERVED
PAYMENT_PROCESSING
PAYMENT_UNKNOWN
PAYMENT_FAILED
CONFIRMED
CANCELLED
EXPIRED
```

The service application layer owns valid state transitions.

## booking_prices

Pricing values become an immutable snapshot once the user accepts a quote.

```sql
CREATE TABLE booking_prices (
    booking_id UUID PRIMARY KEY,
    room_rate BIGINT NOT NULL,
    nights INT NOT NULL,
    subtotal BIGINT NOT NULL,
    tax BIGINT NOT NULL,
    service_fee BIGINT NOT NULL,
    discount BIGINT NOT NULL DEFAULT 0,
    total BIGINT NOT NULL,
    currency VARCHAR(3) NOT NULL,
    pricing_version VARCHAR(100),
    quoted_at TIMESTAMPTZ NOT NULL
);
```

Monetary values use the currency's smallest unit, for example cents, and must not use floating-point types.

Optional future breakdown:

```sql
CREATE TABLE booking_price_items (
    id UUID PRIMARY KEY,
    booking_id UUID NOT NULL,
    type VARCHAR(50) NOT NULL,
    description TEXT,
    amount BIGINT NOT NULL
);
```

## booking_idempotency_keys

```sql
CREATE TABLE booking_idempotency_keys (
    id UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    request_hash VARCHAR(255) NOT NULL,
    booking_id UUID,
    status VARCHAR(50) NOT NULL,
    response JSONB,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ
);
```

Behavior:

```text
same key + same request    -> return previous/in-progress result
same key + different hash -> reject request
new key                    -> start CreateBooking
```

The unique constraint is the final concurrency guard against duplicate request creation.

## booking_sagas

Saga state is persisted so Booking Service can resume after a process crash instead of restarting the workflow from scratch.

```sql
CREATE TABLE booking_sagas (
    id UUID PRIMARY KEY,
    booking_id UUID NOT NULL UNIQUE,
    state VARCHAR(80) NOT NULL,
    reservation_id UUID,
    payment_id UUID,
    last_error TEXT,
    retry_count INT NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

Possible states:

```text
STARTED
PRICE_QUOTED
INVENTORY_RESERVED
PAYMENT_PROCESSING
PAYMENT_SUCCEEDED
PAYMENT_UNKNOWN
CONFIRMING_RESERVATION
COMPLETED
COMPENSATING
COMPENSATED
FAILED
```

## booking_outbox_events

```sql
CREATE TABLE booking_outbox_events (
    id UUID PRIMARY KEY,
    aggregate_type VARCHAR(50) NOT NULL,
    aggregate_id UUID NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(30) NOT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ
);
```

Recommended index:

```sql
CREATE INDEX idx_booking_outbox_pending
ON booking_outbox_events(status, next_retry_at);
```

Booking state changes and their outbox events must be committed in the same local transaction.

---

# 2. Availability Service

Availability Service owns the invariant:

> A room type must never be oversold for any date.

Therefore it owns date-based inventory and reservation holds.

## room_inventory

```sql
CREATE TABLE room_inventory (
    hotel_id UUID NOT NULL,
    room_type_id UUID NOT NULL,
    inventory_date DATE NOT NULL,
    total_quantity INT NOT NULL,
    reserved_quantity INT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (hotel_id, room_type_id, inventory_date)
);
```

Available quantity is derived as:

```text
total_quantity - reserved_quantity
```

The initial implementation should use PostgreSQL as the source of truth. Redis locks are not required for correctness in the MVP.

## reservations

```sql
CREATE TABLE reservations (
    id UUID PRIMARY KEY,
    booking_id UUID NOT NULL UNIQUE,
    hotel_id UUID NOT NULL,
    room_type_id UUID NOT NULL,
    check_in DATE NOT NULL,
    check_out DATE NOT NULL,
    quantity INT NOT NULL,
    status VARCHAR(30) NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

Suggested statuses:

```text
HELD
BOOKED
RELEASED
EXPIRED
```

`booking_id` is unique so repeating `ReserveInventory` for the same booking cannot create a second reservation.

## reservation_inventory

```sql
CREATE TABLE reservation_inventory (
    reservation_id UUID NOT NULL,
    inventory_date DATE NOT NULL,
    quantity INT NOT NULL,
    PRIMARY KEY (reservation_id, inventory_date)
);
```

This table records the exact inventory rows consumed by a reservation, making confirmation, expiration, and release deterministic and idempotent.

## Multi-night reservation transaction

A stay from September 1 to September 4 consumes September 1, 2, and 3.

Initial locking strategy:

```sql
SELECT *
FROM room_inventory
WHERE hotel_id = $1
  AND room_type_id = $2
  AND inventory_date >= $3
  AND inventory_date < $4
ORDER BY inventory_date
FOR UPDATE;
```

The transaction then verifies all required dates have enough remaining quantity, updates every row, inserts the reservation, and inserts `reservation_inventory` rows.

If any date cannot satisfy the quantity, the transaction rolls back completely.

Ordering by date ensures concurrent transactions acquire row locks in a consistent order and reduces deadlock risk.

## Future optimization

A later version can benchmark pessimistic locking against conditional atomic updates such as:

```sql
UPDATE room_inventory
SET reserved_quantity = reserved_quantity + $1,
    version = version + 1
WHERE hotel_id = $2
  AND room_type_id = $3
  AND inventory_date = $4
  AND reserved_quantity + $1 <= total_quantity;
```

The MVP should favor correctness and clear reasoning before optimizing contention.

## Reservation expiry worker

Expired holds can be claimed with:

```sql
SELECT *
FROM reservations
WHERE status = 'HELD'
  AND expires_at <= NOW()
ORDER BY expires_at
LIMIT 100
FOR UPDATE SKIP LOCKED;
```

`SKIP LOCKED` allows multiple Go workers to safely process expired reservations in parallel.

---

# 3. Pricing Service

Pricing Service owns mutable rate configuration and pricing rules. Booking Service owns only the accepted immutable quote snapshot.

## room_rates

```sql
CREATE TABLE room_rates (
    id UUID PRIMARY KEY,
    hotel_id UUID NOT NULL,
    room_type_id UUID NOT NULL,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    nightly_rate BIGINT NOT NULL,
    currency VARCHAR(3) NOT NULL,
    priority INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

Optional later model:

```sql
CREATE TABLE pricing_rules (
    id UUID PRIMARY KEY,
    hotel_id UUID NOT NULL,
    room_type_id UUID,
    rule_type VARCHAR(50) NOT NULL,
    start_date DATE,
    end_date DATE,
    adjustment_type VARCHAR(30) NOT NULL,
    adjustment_value BIGINT NOT NULL,
    priority INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

Examples of future rule types include weekend surcharge, seasonal pricing, promotion, and occupancy-based adjustments.

---

# 4. Payment Service

Payment Service owns the invariant:

> A logical booking payment must never be charged more than intended because of network retries.

## payments

```sql
CREATE TABLE payments (
    id UUID PRIMARY KEY,
    booking_id UUID NOT NULL UNIQUE,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    amount BIGINT NOT NULL,
    currency VARCHAR(3) NOT NULL,
    status VARCHAR(30) NOT NULL,
    provider VARCHAR(50),
    provider_payment_id VARCHAR(255),
    failure_code VARCHAR(100),
    failure_message TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

Suggested statuses:

```text
PENDING
PROCESSING
SUCCEEDED
FAILED
UNKNOWN
REFUNDED
```

A gRPC timeout from Payment Service must not automatically be interpreted as `FAILED` by Booking Service.

## payment_attempts

```sql
CREATE TABLE payment_attempts (
    id UUID PRIMARY KEY,
    payment_id UUID NOT NULL,
    attempt_number INT NOT NULL,
    provider_request_id VARCHAR(255),
    status VARCHAR(30) NOT NULL,
    request_payload JSONB,
    response_payload JSONB,
    error_code VARCHAR(100),
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ
);
```

This preserves retry history and makes payment reconciliation observable.

## payment_webhook_events

For a Stripe-like provider simulation:

```sql
CREATE TABLE payment_webhook_events (
    provider_event_id VARCHAR(255) PRIMARY KEY,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    processed BOOLEAN NOT NULL DEFAULT FALSE,
    received_at TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ
);
```

The primary key provides webhook idempotency.

---

# 5. Catalog Service

Catalog Service owns descriptive hotel data. Availability Service must not own hotel metadata merely because it manages room inventory.

## hotels

```sql
CREATE TABLE hotels (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    address_line TEXT,
    city VARCHAR(120),
    country_code VARCHAR(2),
    status VARCHAR(30) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

## room_types

```sql
CREATE TABLE room_types (
    id UUID PRIMARY KEY,
    hotel_id UUID NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    max_guests INT NOT NULL,
    status VARCHAR(30) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

Catalog may later own amenities, photos, policies, and searchable hotel metadata.

Availability and Pricing refer to `hotel_id` and `room_type_id` as opaque IDs. They do not have database foreign keys to Catalog.

---

# 6. Auth Service

The MVP keeps Auth deliberately simple.

## users

```sql
CREATE TABLE users (
    id UUID PRIMARY KEY,
    email VARCHAR(320) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    first_name VARCHAR(120),
    last_name VARCHAR(120),
    status VARCHAR(30) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

## refresh_tokens

```sql
CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);
```

The API Gateway validates external JWT access tokens in the MVP. Service-to-service authentication can be added later.

---

# 7. Notification Service

Notification Service consumes events and owns notification delivery state. It should not query Booking, User, or Catalog databases directly.

Events intended for Notification should carry the stable snapshot required for the message.

Example `BookingConfirmed` payload:

```json
{
  "event_id": "evt_123",
  "booking_id": "booking_123",
  "user_id": "user_123",
  "email": "guest@example.com",
  "hotel_name": "Example Hotel",
  "check_in": "2026-09-01",
  "check_out": "2026-09-04",
  "total": 34000,
  "currency": "USD"
}
```

## notifications

```sql
CREATE TABLE notifications (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL UNIQUE,
    user_id UUID,
    type VARCHAR(50) NOT NULL,
    channel VARCHAR(30) NOT NULL,
    recipient TEXT NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(30) NOT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    sent_at TIMESTAMPTZ
);
```

`event_id` is unique because Kafka consumers must assume at-least-once delivery.

---

# 8. Data ownership map

| Data | Owning service |
|---|---|
| User/account | Auth |
| Hotel metadata | Catalog |
| Room type metadata | Catalog |
| Room inventory by date | Availability |
| Reservation / temporary hold | Availability |
| Mutable room rates | Pricing |
| Pricing rules | Pricing |
| Booking | Booking |
| Accepted booking price snapshot | Booking |
| Booking Saga progress | Booking |
| Booking request idempotency | Booking |
| Payment | Payment |
| Payment attempts | Payment |
| Provider webhook deduplication | Payment |
| Outbox events | The service producing the event |
| Email/SMS delivery state | Notification |

---

# 9. No cross-service foreign keys

IDs from another bounded context are stored as opaque references.

For example, Booking Service may store:

```text
hotel_id
room_type_id
user_id
```

but must not define a database foreign key into Catalog or Auth databases.

Correctness across services is enforced through service contracts, local invariants, idempotency, and event-driven consistency rather than shared relational constraints.

---

# 10. Business invariant ownership

Service boundaries are justified by invariants.

## Availability

Invariant:

```text
Inventory must never be oversold.
```

Owner:

```text
Availability Service + availability_db transaction
```

## Payment

Invariant:

```text
A logical payment must not be charged twice because a request was retried.
```

Owner:

```text
Payment Service + unique payment idempotency key
```

## Booking

Invariant:

```text
A confirmed booking must eventually publish BookingConfirmed.
```

Owner:

```text
Booking Service + local booking/outbox transaction
```

This principle should guide future service decomposition.

---

# 11. Logical database architecture

```text
                         API Gateway
                              |
                             gRPC
                              |
          +-------------------+-------------------+
          |         |          |         |        |
          v         v          v         v        v
       Catalog   Booking    Pricing  Availability Payment
          |         |          |         |        |
          v         v          v         v        v
     catalog_db booking_db pricing_db avail_db payment_db
                    |
                  outbox
                    |
                    v
                  Kafka
                    |
              +-----+------+
              |            |
              v            v
        Notification     Audit/Analytics
              |
              v
      notification_db
```

---

# 12. Initial indexing notes

The exact index set should be validated with queries and load tests, but the MVP should at least consider:

```text
Booking
- bookings(user_id, created_at DESC)
- bookings(status, updated_at)
- booking_sagas(state, next_retry_at)
- booking_outbox_events(status, next_retry_at)

Availability
- primary key on (hotel_id, room_type_id, inventory_date)
- reservations(status, expires_at)
- reservations(booking_id) UNIQUE

Pricing
- room_rates(hotel_id, room_type_id, start_date, end_date)

Payment
- payments(booking_id) UNIQUE
- payments(idempotency_key) UNIQUE
- payments(status, updated_at)
- payment_attempts(payment_id, attempt_number)

Notification
- notifications(event_id) UNIQUE
- notifications(status, created_at)
```

Indexes should support real access patterns rather than be added speculatively everywhere.

---

# 13. Decisions made

- Use database-per-service as the logical ownership model.
- A single PostgreSQL instance may host multiple service databases during local development.
- No cross-service database reads or foreign keys.
- Add Catalog Service to own hotel and room type metadata.
- Booking Service owns price snapshots and Saga progress.
- Availability Service owns date-based room inventory and reservation holds.
- PostgreSQL is the source of truth for oversell prevention.
- Start with pessimistic row locking for multi-night inventory reservations.
- Payment Service independently enforces payment idempotency.
- Notification consumers enforce event idempotency.
- Each event-producing service owns its own outbox table.
- Store monetary values as integer smallest units rather than floating point.

---

# 14. Open questions

The next iterations should answer:

- Exact `ReserveInventory` SQL transaction and rollback logic.
- Whether inventory uses `reserved_quantity` only or separates held/booked counters.
- How reservation expiry races with payment success.
- Whether Saga state lives in a dedicated table long-term or can be folded into booking workflow records.
- Exact Catalog-to-Availability synchronization when new room types are created.
- Whether Pricing consumes Catalog events or resolves identifiers synchronously.
- Event schema/versioning conventions.
- Retention policies for idempotency, outbox, payment attempts, and webhook records.
- Partitioning/archive strategies if inventory or outbox tables become large.

## Next design step

Design the Availability Service in detail, especially the `ReserveInventory` transaction, concurrency behavior, expiration flow, and associated gRPC contract.