# Create Booking Design

## Purpose

`CreateBooking` is the central business flow of the Hotel Booking Platform. This document defines the first detailed design before database schemas and protobuf contracts are finalized.

The design intentionally focuses on distributed-system behavior: service ownership, Saga orchestration, inventory concurrency, idempotency, payment uncertainty, and reliable event publication.

## High-level flow

```text
Client
  |
  | POST /api/v1/bookings
  v
API Gateway
  |
  | gRPC CreateBooking
  v
Booking Service (Saga Orchestrator)
  |
  +-- create booking: PENDING
  |
  +-- gRPC Quote ----------------------> Pricing Service
  |
  +-- gRPC ReserveInventory ----------> Availability Service
  |                                      |
  |                                      +-- atomically reserve inventory
  |                                      +-- reservation: HELD
  |                                      +-- expires_at
  |
  +-- gRPC CreatePayment -------------> Payment Service
  |                                      |
  |                                      +-- enforce idempotency
  |                                      +-- process payment
  |
  +-- payment success
  |
  +-- gRPC ConfirmReservation --------> Availability Service
  |
  +-- booking: CONFIRMED
  +-- insert BookingConfirmed into outbox
  |
  v
Response
```

## Saga strategy

The MVP uses an **orchestration Saga**. `Booking Service` is the orchestrator and owns the booking workflow.

A distributed database transaction must not span Booking, Availability, Pricing, or Payment. Each service owns and commits its own local transaction.

```text
                    Booking Service
                    Saga Orchestrator
                         /   |   \
                        /    |    \
                    gRPC   gRPC   gRPC
                      /      |      \
                     v       v       v
                 Pricing Availability Payment
```

This approach is preferred initially over pure event choreography because the main booking workflow is easier to understand, test, recover, and explain.

## Booking states

Proposed detailed lifecycle:

```text
PENDING
   |
   v
INVENTORY_RESERVED
   |
   v
PAYMENT_PROCESSING
   |
   +---- success ----> CONFIRMED
   |
   +---- failure ----> PAYMENT_FAILED
   |
   +---- uncertain --> PAYMENT_UNKNOWN
```

Additional terminal states may include:

- `CANCELLED`
- `EXPIRED`

The exact state model will be refined when persistence and Saga recovery are designed.

## Reservation states

Availability Service owns reservation state.

```text
HELD
 |
 +---- Confirm ----> BOOKED
 |
 +---- Release ----> RELEASED
 |
 +---- TTL --------> EXPIRED
```

Suggested reservation fields:

```text
reservation_id
booking_id
hotel_id
room_type_id
check_in
check_out
quantity
status
expires_at
created_at
updated_at
```

A temporary hold should have a bounded TTL, initially proposed as 10 minutes. The final value should be configurable rather than hard-coded.

## Inventory model

Hotel inventory is date-based. A booking from September 1 to September 4 consumes inventory for September 1, 2, and 3; checkout day is not consumed.

Initial logical model:

```text
room_inventory
--------------
hotel_id
room_type_id
date
total
reserved
version
```

All dates required by one reservation must succeed in one Availability Service database transaction. If any date is sold out, the whole reservation attempt rolls back.

Example:

```text
Sep 1: available
Sep 2: available
Sep 3: sold out

Result: reservation rejected; no partial inventory hold.
```

## Preventing double booking

Availability Service's database is the source of truth. The first implementation should rely on PostgreSQL atomic updates/transactions instead of introducing a Redis distributed lock.

Conceptually:

```sql
UPDATE room_inventory
SET reserved = reserved + :quantity
WHERE hotel_id = :hotel_id
  AND room_type_id = :room_type_id
  AND date = :date
  AND reserved + :quantity <= total;
```

If the expected rows cannot all be updated, the transaction rolls back and the service returns a sold-out result.

This prevents two concurrent requests from successfully consuming the final inventory unit.

## Pricing snapshot

A price shown during search must not silently change while payment is being processed.

At booking time, Pricing Service returns a quote. Booking Service persists an immutable price snapshot associated with the booking.

Example:

```text
room_price:    100
nights:          3
subtotal:      300
tax:            30
service_fee:    10
------------------
total:         340
currency:      USD
```

Payment Service receives the amount from the accepted booking snapshot instead of recalculating the price independently.

## CreateBooking idempotency

The external API accepts an idempotency key:

```text
POST /api/v1/bookings
Idempotency-Key: <unique-client-key>
```

Booking Service associates the key with the request and resulting booking. Retrying the same logical request with the same key must return the existing result rather than creating another booking.

The idempotency record should eventually contain enough information to detect a key reused with a different request, for example:

```text
idempotency_key
request_hash
booking_id
status
response
created_at
```

## Payment idempotency

Payment is independently idempotent because network retries must never cause duplicate charges.

A stable payment idempotency key can be derived from the booking/payment attempt, for example:

```text
payment:{booking_id}
```

If Payment Service has already successfully processed that key, subsequent calls return the existing payment result.

## Payment timeout and uncertainty

A gRPC timeout does **not** prove that payment failed.

Example:

```text
Booking Service ---- CreatePayment ----> Payment Service
                                         |
                                         +--> provider succeeds
                                         |
                         response -------X network/deadline
```

Booking Service must not immediately release inventory in this case because the customer may already have been charged.

Payment state should distinguish at least:

- `PENDING`
- `PROCESSING`
- `SUCCEEDED`
- `FAILED`
- `UNKNOWN`

For an uncertain result, the Saga moves to `PAYMENT_UNKNOWN` and reconciles via `GetPayment` and/or a later authoritative payment event.

## Payment failure compensation

For a confirmed payment failure:

```text
Payment FAILED
     |
     v
Booking Service
     |
     +-- gRPC ReleaseReservation
     v
Availability Service
     |
     +-- HELD -> RELEASED

Booking Service
     |
     +-- booking -> PAYMENT_FAILED
```

Compensation commands must themselves be idempotent so retries are safe.

## gRPC responsibilities

Synchronous calls are used for operations where the booking workflow needs an immediate result.

Expected Availability API:

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
}
```

Expected Pricing API:

```proto
service PricingService {
  rpc Quote(QuoteRequest)
      returns (QuoteResponse);
}
```

Expected Payment API:

```proto
service PaymentService {
  rpc CreatePayment(CreatePaymentRequest)
      returns (CreatePaymentResponse);

  rpc GetPayment(GetPaymentRequest)
      returns (GetPaymentResponse);

  rpc RefundPayment(RefundPaymentRequest)
      returns (RefundPaymentResponse);
}
```

These definitions are conceptual. Exact protobuf messages, error codes, versioning, deadlines, and retry policies will be designed separately.

## Kafka responsibilities

Kafka is not required to orchestrate every synchronous step of the initial booking flow.

Initial rule:

```text
Immediate workflow decision -> gRPC
Domain event / side effect   -> Kafka
```

Candidate events:

- `BookingConfirmed`
- `BookingCancelled`
- `PaymentSucceeded`
- `PaymentFailed`
- `ReservationExpired`

Example consumers of `BookingConfirmed`:

```text
BookingConfirmed
       |
       +--> Notification Service
       +--> Audit consumer
       +--> Analytics consumer
```

Consumers must assume at-least-once delivery and therefore be idempotent.

## Transactional Outbox

Booking confirmation and event publication cannot rely on a database commit followed by a best-effort Kafka publish.

Instead:

```text
BEGIN

UPDATE booking -> CONFIRMED
INSERT outbox_event -> BookingConfirmed

COMMIT
```

A separate outbox publisher sends pending events to Kafka and marks them published after successful publication.

This ensures that a committed booking eventually produces its domain event even if Kafka is temporarily unavailable.

## External API draft

```http
POST /api/v1/bookings
Authorization: Bearer <token>
Idempotency-Key: <unique-key>
```

Example request:

```json
{
  "hotel_id": "hotel_001",
  "room_type_id": "deluxe",
  "check_in": "2026-09-01",
  "check_out": "2026-09-04",
  "guests": 2,
  "rooms": 1,
  "payment_method_id": "pm_123"
}
```

Example successful response:

```json
{
  "booking_id": "booking_001",
  "status": "CONFIRMED",
  "reservation_id": "reservation_001",
  "payment_id": "payment_001",
  "price": {
    "subtotal": 300,
    "tax": 30,
    "service_fee": 10,
    "total": 340,
    "currency": "USD"
  }
}
```

The public REST contract is a draft and may change after asynchronous payment handling is finalized.

## Important failure scenarios

The implementation and tests should eventually cover at least:

1. Two users attempt to reserve the final room concurrently.
2. One date in a multi-night reservation is sold out.
3. Client retries `CreateBooking` after an HTTP timeout.
4. Booking Service retries a payment request after a lost gRPC response.
5. Payment succeeds but Booking Service receives `DeadlineExceeded`.
6. Booking Service crashes after payment succeeds but before confirming the booking.
7. Reservation expires while payment is still processing.
8. `ReleaseReservation` is called multiple times.
9. Booking DB commits but Kafka is unavailable.
10. Kafka delivers `BookingConfirmed` more than once.

These scenarios are first-class design requirements, not optional edge cases.

## Current architecture

```text
                         Client
                           |
                          REST
                           |
                           v
                     API Gateway
                           |
                          gRPC
                           |
                           v
                   Booking Service
                   Saga Orchestrator
                    /      |       \
                  gRPC    gRPC      gRPC
                  /        |          \
                 v         v           v
             Pricing  Availability   Payment
                         Service      Service
                            |
                         PostgreSQL

                   Booking Service
                         |
                     PostgreSQL
                         |
                       Outbox
                         |
                         v
                       Kafka
                         |
                +--------+---------+
                |        |         |
                v        v         v
          Notification  Audit   Analytics
```

## Decisions made so far

- Booking Service orchestrates the initial booking Saga.
- Service-local transactions only; no distributed database transaction.
- PostgreSQL is the Availability source of truth.
- Inventory reservation must be atomic across all requested stay dates.
- Redis locks are not required for the initial concurrency solution.
- Pricing is snapshotted before payment.
- Booking creation and payment both require idempotency.
- Payment timeout is treated as an uncertain result, not an automatic failure.
- gRPC is used for synchronous internal operations.
- Kafka is used for domain events and asynchronous side effects.
- Transactional Outbox provides reliable event publication.

## Open questions

The following decisions intentionally remain open for the next design iterations:

- Exact database ownership and schemas per service.
- Whether booking confirmation waits synchronously for `ConfirmReservation` after payment.
- Saga persistence/recovery model after Booking Service restart.
- Reservation expiry mechanism and race handling with in-flight payment.
- Payment provider simulation and reconciliation strategy.
- gRPC status/error model and retry policies.
- Kafka topic structure, partition keys, event schemas, and versioning.
- Cancellation/refund policy.

## Next design step

Design **data ownership and database schemas per service**. Once persistence boundaries are clear, define the protobuf contracts and detailed Saga recovery behavior.