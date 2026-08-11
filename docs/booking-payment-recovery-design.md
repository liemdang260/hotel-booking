# Booking, Pricing, and Payment Recovery Design

## Purpose

This document defines the durable interaction between Booking, Pricing, Availability, and Payment for the hotel booking platform. The design focuses on crash recovery, idempotency, ambiguous payment outcomes, and compensation.

The implementation must follow the project Clean Architecture convention:

```text
Application entry point
        ↓
Usecase
        ↓
Repository / gateway interfaces
        ↓
Infrastructure adapters
```

Application code must not contain business decisions. Usecases must not depend directly on protobuf, gRPC generated clients, pgx, Kafka clients, Redis clients, or payment-provider SDKs.

---

## 1. Booking Service is the Saga orchestrator

Booking Service owns the end-to-end booking workflow:

```text
Create booking
    ↓
Quote price
    ↓
Reserve inventory
    ↓
Process payment
    ↓
Confirm reservation
    ↓
Confirm booking
```

Pricing, Availability, and Payment expose business capabilities but do not coordinate the overall booking flow.

Booking Service is therefore responsible for:

- durable Saga progress
- retry decisions
- payment reconciliation
- compensation decisions
- final Booking state
- emitting Booking domain events through the outbox

---

## 2. Durable-step rule

Never execute the complete workflow and persist only at the end.

Required pattern:

```text
remote action
    ↓
persist durable result locally
    ↓
remote action
```

Example:

```text
ReserveInventory succeeds
    ↓
Booking persists reservation_id
Saga = INVENTORY_RESERVED
    ↓
CreatePayment
```

This ensures the process can resume after a crash at any boundary.

Remote network calls must not be made while holding a long-lived Booking database transaction.

---

## 3. Booking state vs Saga state

Booking status is customer/business state.
Saga state is internal workflow execution state.

They must remain separate.

### Suggested Booking statuses

```text
PENDING
INVENTORY_RESERVED
PAYMENT_PROCESSING
PAYMENT_UNKNOWN
PAYMENT_FAILED
PENDING_CONFIRMATION
CONFIRMED
CANCELLED
EXPIRED
FAILED
```

### Suggested Saga states

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

`FAILED` is reserved for non-recoverable workflow failure or invariant violation. A transient timeout is not automatically `FAILED`.

---

## 4. Pricing design

Pricing exposes a synchronous Quote capability.

Conceptual contract:

```text
Quote(
  hotel_id,
  room_type_id,
  check_in,
  check_out,
  room_quantity,
  guest_count
)
```

Quote result:

```text
quote_id
currency
subtotal
tax
service_fee
discount
total
pricing_version
quoted_at
expires_at (optional)
```

All money values use integer minor units. Floating point must not be used for money.

Example:

```json
{
  "quote_id": "q_123",
  "currency": "USD",
  "subtotal": 30000,
  "tax": 3000,
  "service_fee": 1000,
  "discount": 0,
  "total": 34000,
  "pricing_version": "2026-08-v3"
}
```

### Immutable booking price snapshot

Once Booking accepts a quote, it persists an immutable price snapshot before continuing the Saga.

Payment must charge the stored snapshot total. Booking must not re-query Pricing immediately before payment because the price may have changed.

Rule:

> One booking attempt uses one immutable accepted price snapshot.

### Pricing retry semantics

Quote has no financial side effect and can use bounded retry on transient transport failures.

Pricing retry behavior must remain bounded by the caller deadline.

---

## 5. Availability idempotency

`ReserveInventory` uses `booking_id` as the logical command identity.

Repeated calls for the same booking and same reservation request must return the same logical reservation instead of consuming inventory again.

Example:

```text
ReserveInventory(booking_id=B123)
    ↓
reservation_id=R456

network response lost

ReserveInventory(booking_id=B123)
    ↓
reservation_id=R456
```

Availability must enforce this with a database unique constraint on `booking_id`, not only an application pre-check.

Confirm and release commands must also be idempotent desired-state operations.

---

## 6. Payment logical identity

A Booking must have at most one logical payment operation.

Recommended Payment constraints:

```text
payments.booking_id UNIQUE
payments.idempotency_key UNIQUE
```

Recommended command identity:

```text
payment:<booking_id>
```

Payment states:

```text
PENDING
PROCESSING
SUCCEEDED
FAILED
UNKNOWN
REFUNDED
```

`UNKNOWN` is a first-class state.

---

## 7. Payment timeout is not payment failure

A timeout only means the caller does not know the result.

Example:

```text
Booking -> Payment -> Provider
                    charge succeeds
Payment persists SUCCEEDED
response to Booking is lost
Booking receives DeadlineExceeded
```

Booking must not interpret this as a confirmed payment failure.

Correct interpretation:

```text
DeadlineExceeded / ambiguous transport failure
    ↓
PAYMENT_UNKNOWN
```

Booking must not immediately release the reservation or issue a second unrelated payment.

---

## 8. Payment Service Clean Architecture

Suggested inner interfaces:

```go
type PaymentRepository interface {
    // persistence operations
}

type PaymentProvider interface {
    Charge(ctx context.Context, input ChargeInput) (ChargeResult, error)
    GetPayment(ctx context.Context, input GetPaymentInput) (ProviderPayment, error)
    Refund(ctx context.Context, input RefundInput) (RefundResult, error)
}
```

Provider SDK usage belongs in infrastructure adapters only.

Usecases own decisions such as:

- whether a payment may be created
- whether an existing payment should be returned
- whether a provider timeout creates UNKNOWN
- whether reconciliation is required
- whether a refund is allowed

---

## 9. Payment attempts

Maintain a separate `payment_attempts` audit trail.

Suggested fields:

```text
id
payment_id
attempt_number
operation
provider_request_id
status
request_payload
response_payload
error_code
started_at
completed_at
```

This makes timeout/retry/reconciliation behavior observable without overloading the main payment record.

---

## 10. CreateBooking payment flow

Starting state:

```text
Saga = INVENTORY_RESERVED
```

Booking first persists:

```text
Booking = PAYMENT_PROCESSING
Saga = PAYMENT_PROCESSING
```

Then it calls Payment.

### Payment succeeds

Persist:

```text
payment_id
Booking = PENDING_CONFIRMATION
Saga = PAYMENT_SUCCEEDED
```

Then continue to ConfirmReservation.

### Payment is definitively declined / failed

Persist:

```text
payment_id
Booking = PAYMENT_FAILED
Saga = COMPENSATING
```

Then release the reservation.

### Payment outcome is ambiguous

Persist:

```text
Booking = PAYMENT_UNKNOWN
Saga = PAYMENT_UNKNOWN
retry_count += 1
next_retry_at = ...
```

Do not release inventory solely because of transport ambiguity.

---

## 11. Recovery worker

The recovery worker is an Application entry point only.

```text
application/worker
    ↓
RecoverBookingSagaUsecase
    ↓
repositories / gateway interfaces
```

The worker must not contain Saga decision logic or direct SQL.

Recovery candidates include non-terminal states such as:

```text
PAYMENT_UNKNOWN
PAYMENT_SUCCEEDED
CONFIRMING_RESERVATION
COMPENSATING
```

---

## 12. Recovery algorithm

Conceptual algorithm:

```text
PAYMENT_UNKNOWN
    ↓
GetPayment
    ├── SUCCEEDED -> persist PAYMENT_SUCCEEDED -> confirm reservation
    ├── FAILED    -> persist COMPENSATING -> release reservation
    └── UNKNOWN / PROCESSING -> schedule another bounded retry

PAYMENT_SUCCEEDED
    ↓
ConfirmReservation

CONFIRMING_RESERVATION
    ↓
retry idempotent ConfirmReservation

COMPENSATING
    ↓
resume required compensation action
```

Every downstream mutation command used in recovery must be idempotent.

---

## 13. Crash recovery scenarios

### Crash after inventory reservation succeeds but before Booking saves reservation_id

Availability already holds inventory, but Booking does not know the reservation ID.

Recovery repeats:

```text
ReserveInventory(booking_id)
```

Availability returns the existing reservation. Booking persists the recovered ID and continues.

### Crash after payment succeeds but before Booking saves the result

Payment has authoritative `SUCCEEDED`; Booking still has `PAYMENT_PROCESSING`.

Recovery uses the same logical payment identity and reads/reuses existing payment state.

It must not create a new independent charge.

### Crash after ConfirmReservation succeeds but before Booking becomes CONFIRMED

Availability already has `BOOKED`; Booking remains in `CONFIRMING_RESERVATION`.

Recovery repeats ConfirmReservation. Availability returns success for the already-booked reservation, after which Booking can finalize locally.

---

## 14. Reservation expiry during payment uncertainty

Important scenario:

```text
10:00 reservation HELD
10:09 payment starts
10:10 reservation expires
10:11 payment reconciliation reports SUCCEEDED
```

Booking now has:

```text
Payment = SUCCEEDED
Reservation = EXPIRED
```

It cannot confirm the reservation.

Required Saga path:

```text
PAYMENT_SUCCEEDED
    ↓
ConfirmReservation
    ↓
RESERVATION_EXPIRED
    ↓
COMPENSATING
    ↓
RefundPayment
    ↓
COMPENSATED
```

The booking remains unsuccessful and stores a concrete failure reason.

---

## 15. Compensation is durable workflow

Compensation must not be implemented only as `defer` or best-effort cleanup.

Compensation may fail or time out and therefore requires persisted workflow state.

Typical cases:

### Confirmed payment failure while inventory is HELD

```text
ReleaseReservation
```

### Payment succeeded but inventory is no longer confirmable

```text
RefundPayment
```

### ConfirmReservation has only a transient error

Retry confirmation first. Do not refund merely because confirmation temporarily timed out.

Refund only after the inventory outcome is known to be non-recoverable.

---

## 16. Retry classification

### Generally retryable

```text
Unavailable
DeadlineExceeded for non-financial/idempotent calls
transient DB failure
Kafka unavailable
other explicitly transient infrastructure errors
```

### Generally non-retryable

```text
InvalidArgument
SoldOut
ReservationExpired
IdempotencyConflict
confirmed PaymentDeclined
```

### Ambiguous financial case

```text
Payment provider / Payment RPC timeout
```

This must enter reconciliation, not blind charge retry.

---

## 17. Backoff

Saga persistence includes:

```text
retry_count
next_retry_at
```

Use bounded exponential-style backoff with jitter in production.

An MVP can use a documented sequence such as:

```text
5s
15s
60s
5m
```

Retry must always respect an overall retry policy and terminal/manual-review threshold.

---

## 18. Concurrent recovery workers

Multiple Booking instances may run recovery workers.

Do not hold a database row lock while making a remote network call.

Recommended approach:

1. claim/reload Saga state locally
2. commit
3. perform remote idempotent action
4. persist result using optimistic versioning

`booking_sagas.version` can be updated conditionally:

```sql
UPDATE booking_sagas
SET state = $new_state,
    version = version + 1,
    updated_at = NOW()
WHERE id = $id
  AND version = $expected_version;
```

Zero affected rows means another worker changed the Saga first; reload instead of overwriting it.

Redis distributed locking is not required for this workflow initially.

---

## 19. Final Booking confirmation and outbox

Booking finalization and `BookingConfirmed` outbox insertion must happen in one local transaction:

```text
BEGIN

booking.status = CONFIRMED
saga.state = COMPLETED
INSERT BookingConfirmed into outbox

COMMIT
```

This guarantees a confirmed booking cannot be committed without a durable event waiting to be published.

---

## 20. BookingConfirmed event payload

The event should contain enough snapshot data for common consumers so Notification does not query Booking's private database.

Example:

```json
{
  "event_id": "...",
  "event_type": "BookingConfirmed",
  "event_version": 1,
  "occurred_at": "...",
  "booking": {
    "booking_id": "...",
    "user_id": "...",
    "hotel_id": "...",
    "room_type_id": "...",
    "check_in": "2026-09-01",
    "check_out": "2026-09-04",
    "total_amount": 34000,
    "currency": "USD"
  }
}
```

---

## 21. Booking outbound boundaries

Booking Usecases depend on inner interfaces:

```text
PricingRepository
AvailabilityRepository
PaymentRepository
BookingRepository
SagaRepository
OutboxRepository
```

Infrastructure provides concrete adapters:

```text
PricingRepository       -> gRPC Pricing adapter
AvailabilityRepository  -> gRPC Availability adapter
PaymentRepository       -> gRPC Payment adapter
Booking/Saga/Outbox     -> PostgreSQL adapters
```

Usecases must never instantiate generated gRPC clients directly.

---

## 22. Architectural decisions

The following decisions are considered baseline unless superseded by an ADR:

1. Booking Service is the Saga orchestrator.
2. Saga progress is durably persisted after each meaningful remote step.
3. Remote calls are not made inside long-lived local DB transactions.
4. Accepted pricing is stored as an immutable Booking price snapshot.
5. Availability reservation commands are idempotent by booking identity.
6. Payment is idempotent by booking/payment idempotency identity.
7. Payment timeout or lost response means UNKNOWN until reconciled.
8. Booking recovery resumes from persisted Saga state.
9. Recovery relies on idempotent downstream commands.
10. Compensation is itself durable and retryable.
11. Concurrent Saga recovery uses optimistic state/version control rather than Redis locks initially.
12. Booking CONFIRMED and BookingConfirmed outbox insertion are atomic in one Booking DB transaction.

---

## 23. Implementation consequences

Implementation tickets should not be asked to redesign these behaviors. They should implement the approved boundaries and acceptance criteria.

Likely implementation work includes:

- Pricing quote contract and immutable snapshot support
- Payment idempotency and payment attempt persistence
- explicit Payment UNKNOWN state and reconciliation usecase
- durable Booking Saga recovery worker/usecase
- optimistic Saga version handling
- compensation/retry integration tests
- BookingConfirmed outbox transaction

Any implementation discovery that requires changing these architectural decisions must be raised separately rather than silently changed inside a coding PR.
