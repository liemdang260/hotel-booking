# Availability Service Design

## Purpose

Availability Service owns hotel room inventory and the concurrency rules that prevent overbooking. It is the source of truth for room availability, temporary reservation holds, confirmation, release, and expiration.

The central invariant is:

> Reserved inventory must never exceed total inventory for any hotel, room type, or date.

## Responsibilities

Availability Service owns:

- Availability checks
- Temporary inventory holds
- Reservation confirmation
- Reservation release
- Reservation expiration
- Multi-night inventory consistency
- Concurrency control for reservation operations

It does not own hotel metadata, pricing, booking lifecycle, payment, or notification data.

## Core data model

```text
room_inventory
reservations
reservation_inventory
```

Inventory is tracked by room type and date rather than by physical room number in the MVP.

Example:

```text
hotel_id | room_type | date       | total | reserved
----------------------------------------------------
H1       | DELUXE    | 2026-09-01 | 5     | 3
H1       | DELUXE    | 2026-09-02 | 5     | 4
H1       | DELUXE    | 2026-09-03 | 5     | 2
```

Available quantity is `total - reserved`.

## Multi-night rule

A booking from September 1 to September 4 consumes inventory for September 1, 2, and 3. Checkout day is not consumed.

All required dates must succeed in one transaction. If any date is unavailable or missing from inventory configuration, the whole operation fails and rolls back.

## CheckAvailability

`CheckAvailability` is a read-only operation and may be cached later. It is advisory only and does not guarantee that inventory will still exist when `ReserveInventory` runs.

Conceptual query:

```sql
SELECT MIN(total_quantity - reserved_quantity)
FROM room_inventory
WHERE hotel_id = $1
  AND room_type_id = $2
  AND inventory_date >= $3
  AND inventory_date < $4;
```

Correctness is enforced in `ReserveInventory`, not in `CheckAvailability`.

## ReserveInventory

`ReserveInventory` is the critical mutation.

Flow:

```text
validate request
      |
      v
lookup reservation by booking_id
      |
      +-- exists --> return existing reservation
      |
      v
begin transaction
      |
      v
lock inventory rows in date order
      |
      v
validate expected number of rows
      |
      v
validate quantity for every date
      |
      v
increment reserved inventory
      |
      v
create HELD reservation
      |
      v
create reservation_inventory rows
      |
      v
commit
```

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

Rows are locked in a stable date order to reduce deadlock risk.

The implementation must also verify that the number of returned inventory rows equals the requested number of nights. Missing rows mean `INVENTORY_NOT_CONFIGURED`, not available inventory.

## Double booking prevention

Two clients may attempt to reserve the last room concurrently. PostgreSQL row locking is the initial source-of-truth concurrency mechanism. A Redis distributed lock is intentionally not required in the MVP.

The invariant must remain true under concurrency:

```text
reserved_quantity <= total_quantity
```

Database checks should reinforce this invariant.

## Reservation idempotency

`ReserveInventory` must be safe to retry. `booking_id` is unique in `reservations`.

If a request succeeds but the gRPC response is lost, retrying with the same booking ID returns the existing reservation and does not increment inventory again.

The database unique constraint is the final protection against concurrent duplicate requests.

## Reservation state machine

```text
                 ReserveInventory
                       |
                       v
                     HELD
                   /      \
                  /        \
        ConfirmReservation  ReleaseReservation
               |              |
               v              v
             BOOKED         RELEASED

HELD
 |
 | TTL expiration
 v
EXPIRED
```

Allowed transitions:

- `HELD -> BOOKED`
- `HELD -> RELEASED`
- `HELD -> EXPIRED`

Invalid transitions include:

- `EXPIRED -> BOOKED`
- `RELEASED -> BOOKED`
- `BOOKED -> EXPIRED`

Mutation operations must be idempotent where possible.

## Reservation TTL

A successful hold receives an expiration timestamp, initially proposed as 10 minutes and configurable through service configuration.

```text
status     = HELD
expires_at = now + hold_duration
```

## ConfirmReservation

After successful payment, Booking Service calls `ConfirmReservation`.

The operation changes `HELD -> BOOKED`. It does not increment inventory because inventory was already consumed when the hold was created.

Calling `ConfirmReservation` again on an already `BOOKED` reservation returns success.

## ReleaseReservation

Confirmed payment failure or another compensating action may release a held reservation.

The operation locks the reservation row, verifies the current state, decrements the exact inventory rows recorded in `reservation_inventory`, and changes `HELD -> RELEASED` in one transaction.

Repeated release calls must not decrement inventory multiple times.

## Expiration worker

Expired holds are processed in batches:

```sql
SELECT id
FROM reservations
WHERE status = 'HELD'
  AND expires_at <= NOW()
ORDER BY expires_at
LIMIT 100
FOR UPDATE SKIP LOCKED;
```

`SKIP LOCKED` allows multiple expiration workers to process reservations concurrently without selecting the same rows.

Each expiration transaction releases the reservation's inventory and changes `HELD -> EXPIRED`.

## Confirm-vs-expire race

A critical race exists when payment succeeds near the reservation expiration boundary.

Example:

```text
10:09:59 Payment succeeds
10:10:00 Expiration worker starts
10:10:01 Booking Service calls ConfirmReservation
```

Both confirmation and expiration must lock the same reservation row with `FOR UPDATE`, serializing the state transition.

If confirmation wins, the state becomes `BOOKED` and the expiration worker skips it.

If expiration wins, the state becomes `EXPIRED`; later confirmation must fail with `RESERVATION_EXPIRED`. Booking Service must then compensate a successful payment, typically by refunding it.

## gRPC surface

Conceptual service contract:

```proto
service AvailabilityService {
  rpc CheckAvailability(CheckAvailabilityRequest)
      returns (CheckAvailabilityResponse);

  rpc ReserveInventory(ReserveInventoryRequest)
      returns (ReserveInventoryResponse);

  rpc ConfirmReservation(ConfirmReservationRequest)
      returns (ConfirmReservationResponse);

  rpc ReleaseReservation(ReleaseReservationRequest)
      returns (ReleaseReservationResponse);

  rpc GetReservation(GetReservationRequest)
      returns (GetReservationResponse);
}
```

Exact protobuf messages and error details will be finalized after the SQL schema and transaction contracts are locked down.

## Error model candidates

Business errors include:

- `SOLD_OUT`
- `INVENTORY_NOT_CONFIGURED`
- `RESERVATION_EXPIRED`
- `RESERVATION_ALREADY_RELEASED`
- `INVALID_DATE_RANGE`

The preferred direction is standard gRPC status codes plus structured protobuf error details.

## Database constraints and indexes

Recommended constraints:

```text
room_inventory primary key:
(hotel_id, room_type_id, inventory_date)

reservations:
UNIQUE(booking_id)

CHECK(total_quantity >= 0)
CHECK(reserved_quantity >= 0)
CHECK(reserved_quantity <= total_quantity)
CHECK(check_out > check_in)
CHECK(quantity > 0)
```

Expiration lookup index:

```sql
CREATE INDEX idx_reservations_expiry
ON reservations(status, expires_at);
```

## Retry policy

Operations can be safely retried only because the mutation endpoints are designed to be idempotent:

- `CheckAvailability`
- `GetReservation`
- `ReserveInventory`
- `ConfirmReservation`
- `ReleaseReservation`

A blind retry policy must not be introduced before idempotency guarantees are tested.

## Redis and caching

Redis is not part of the reservation correctness path in the MVP.

A future availability cache may improve read throughput, but `ReserveInventory` must always validate and mutate authoritative PostgreSQL state.

Rule:

```text
Check availability -> cache may be used
Reserve inventory  -> PostgreSQL is mandatory
```

## Observability

Candidate metrics:

```text
availability_reserve_total
availability_reserve_success_total
availability_reserve_sold_out_total
availability_reserve_duration_seconds
reservation_active_total
reservation_expired_total
reservation_release_total
db_lock_wait_duration_seconds
```

Lock wait time is particularly useful for measuring contention under load.

## Concurrency test requirement

A core integration/load test should initialize 10 rooms and issue 100 concurrent reservation requests for one room each.

Expected result:

```text
10 success
90 SOLD_OUT
reserved_quantity = 10
```

Any result with more than 10 successful reservations is a correctness failure.

## Current architecture

```text
                gRPC
                  |
                  v
        Availability Service
                  |
          +-------+---------+
          |                 |
          v                 v
 Reservation Logic    Expiration Worker
          |                 |
          +--------+--------+
                   |
                   v
               PostgreSQL
                   |
          +--------+---------+
          v                  v
   room_inventory       reservations
                              |
                              v
                    reservation_inventory
```

## Decisions made so far

- PostgreSQL is the Availability source of truth.
- Inventory is modeled per hotel, room type, and date.
- Multi-night reservation is atomic across all stay dates.
- Initial concurrency strategy uses PostgreSQL row locks.
- Inventory rows are locked in date order.
- `booking_id` makes reservation creation idempotent.
- Confirm, release, and expire serialize on the reservation row.
- Expiration workers use `FOR UPDATE SKIP LOCKED`.
- Redis is not required for reservation correctness.
- Missing date inventory is an explicit configuration error.

## Next design step

Define the concrete PostgreSQL DDL and exact transaction behavior for:

1. `ReserveInventory`
2. `ConfirmReservation`
3. `ReleaseReservation`
4. Expiration processing

Then finalize the Availability protobuf contract.