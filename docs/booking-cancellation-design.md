# Booking Cancellation and Refund Architecture

## Purpose

This document defines customer-initiated cancellation for confirmed hotel bookings. Cancellation is a separate durable business workflow from CreateBooking compensation.

The design follows the platform Clean Architecture rule:

```text
Application entry point
        ↓
Usecase
        ↓
Repository / gateway interfaces
        ↓
Infrastructure adapters
```

Application handlers/workers must remain thin. Business decisions belong in usecases/domain. No usecase may depend directly on protobuf, gRPC generated clients, pgx, Kafka libraries, Redis clients, or payment-provider SDKs.

---

## 1. Cancellation is not CreateBooking compensation

Two different workflows exist:

```text
CreateBooking failure
    ↓
internal Saga compensation
```

and:

```text
Customer requests cancellation
    ↓
customer cancellation workflow
```

They must not share a single workflow state machine or usecase.

CreateBooking compensation restores the system after an incomplete/failed booking attempt. Customer cancellation changes an already-confirmed business commitment according to accepted commercial terms.

---

## 2. MVP cancellation eligibility

The first implementation allows public cancellation only when:

```text
Booking.status = CONFIRMED
```

Requests while Booking is in states such as:

```text
PENDING
INVENTORY_RESERVED
PAYMENT_PROCESSING
PAYMENT_UNKNOWN
PENDING_CONFIRMATION
```

must not enter customer cancellation. Those states are owned by CreateBooking Saga recovery/compensation.

The API returns a business conflict such as `BOOKING_NOT_CANCELLABLE` when the booking is not eligible.

A future `AbortBookingAttempt` workflow may be designed separately if product requirements need customer cancellation while creation is still in progress.

---

## 3. Public API

Conceptual endpoint:

```http
POST /api/v1/bookings/{booking_id}/cancel
Idempotency-Key: <key>
```

Example request:

```json
{
  "reason": "CHANGE_OF_PLAN"
}
```

The API Gateway authenticates the caller and forwards the user identity, booking ID, idempotency key, and cancellation request to Booking Service.

Booking Service remains the business orchestrator.

---

## 4. Booking status vs cancellation workflow state

Booking customer-facing status and cancellation execution state are separate concepts.

Booking status transition:

```text
CONFIRMED
    ↓
CANCELLED
```

The system may expose a temporary internal state such as cancellation processing, but the recommended model is to keep detailed execution state in the cancellation operation rather than create a large number of Booking statuses.

A failed or rejected cancellation does not change a confirmed booking into a new terminal booking status. The booking remains `CONFIRMED` if policy evaluation rejects the request.

---

## 5. Separate durable cancellation operation

Do not reuse a CreateBooking Saga row for customer cancellation.

Recommended table:

```text
booking_cancellations
---------------------
id
booking_id
idempotency_key
request_hash
state
reason
policy_evaluated_at
refund_amount
currency
refund_id
failure_code
failure_message
retry_count
next_retry_at
version
created_at
updated_at
completed_at
```

Recommended constraints:

```text
UNIQUE(idempotency_key)
```

and a database rule preventing more than one active cancellation workflow for the same booking.

The exact implementation can use a partial unique index over non-terminal states.

---

## 6. Cancellation workflow state machine

Recommended state machine:

```text
STARTED
   ↓
POLICY_APPROVED
   ↓
CANCELLING_RESERVATION
   ↓
RESERVATION_CANCELLED
   ↓
REFUND_PROCESSING
   ├──────────────┐
   ↓              ↓
REFUND_SUCCEEDED REFUND_UNKNOWN
   ↓              │
COMPLETED ◄────────┘
```

Additional terminal states:

```text
POLICY_REJECTED
FAILED
```

`FAILED` is for non-recoverable workflow failure/manual intervention. Transient timeouts do not automatically become FAILED.

---

## 7. Cancellation policy is part of the accepted commercial offer

Cancellation terms affect the commercial rate and therefore belong to Pricing when the quote is produced.

Example:

```text
Flexible rate       = $120/night, free cancellation until deadline
Non-refundable rate = $100/night, no refund after acceptance
```

The exact Quote returns both price and cancellation terms.

Example conceptual quote fragment:

```json
{
  "quote_id": "quote_001",
  "total": 34000,
  "currency": "USD",
  "cancellation_policy": {
    "policy_code": "FLEXIBLE",
    "free_cancel_until": "2026-08-30T09:00:00Z",
    "refund_basis_points": 10000,
    "cancellation_fee": 0
  }
}
```

Pricing owns calculation of the commercial terms. Booking owns enforcement of the terms the customer accepted.

---

## 8. Cancellation policy snapshot

The accepted cancellation policy must be immutable for the booking, just like the accepted price snapshot.

Recommended Booking-owned snapshot:

```text
booking_cancellation_policies
-----------------------------
booking_id
policy_code
free_cancel_until
refund_basis_points
cancellation_fee
currency
pricing_version
created_at
```

If hotel/rate policy changes after booking confirmation, existing bookings keep the originally accepted terms.

Booking must not re-query Pricing at cancellation time to discover a potentially different current policy.

---

## 9. Time semantics

Human hotel policy rules may be authored in hotel-local time, for example:

> Free cancellation until 18:00 two days before check-in.

At quote creation, Pricing resolves this rule into an absolute instant using the hotel timezone.

The booking snapshot stores:

```text
free_cancel_until TIMESTAMPTZ
```

Cancellation compares absolute instants. It does not reinterpret the hotel timezone rule during cancellation processing.

The policy evaluation instant is captured once and persisted:

```text
policy_evaluated_at
```

This prevents a request arriving just before a deadline from changing policy outcome because later workflow steps cross the deadline.

---

## 10. Domain policy evaluation

Cancellation usecase loads:

```text
Booking
CancellationPolicySnapshot
```

and calculates a fixed refund result once.

Conceptual domain API:

```go
type CancellationPolicy struct {
    FreeCancelUntil time.Time
    RefundBPS       int
    CancellationFee int64
    Currency        string
}

func (p CancellationPolicy) CalculateRefund(total int64, evaluatedAt time.Time) (int64, error)
```

Refund amount is persisted in the cancellation workflow and is not recalculated later when retrying remote operations.

SQL/database adapters must not decide the refund amount.

---

## 11. Availability requires a booked-reservation cancellation command

Existing `ReleaseReservation` represents releasing a temporary HELD reservation and must not be reused for a confirmed booking.

Availability adds an explicit command such as:

```proto
rpc CancelBookedReservation(
  CancelBookedReservationRequest
) returns (
  CancelBookedReservationResponse
);
```

This makes state semantics explicit.

---

## 12. Reservation state machine

Existing states:

```text
HELD
BOOKED
RELEASED
EXPIRED
```

Add:

```text
CANCELLED
```

Allowed transitions:

```text
HELD
 ├──> BOOKED
 ├──> RELEASED
 └──> EXPIRED

BOOKED
 └──> CANCELLED
```

`BOOKED -> RELEASED` is intentionally invalid because RELEASED means a temporary hold was released before confirmation.

---

## 13. Inventory transition on booked cancellation

Availability uses the final inventory model:

```text
available = total_quantity - held_quantity - booked_quantity
```

ConfirmReservation:

```text
held_quantity   -= quantity
booked_quantity += quantity
```

CancelBookedReservation:

```text
booked_quantity -= quantity
```

The cancellation operation must not alter `held_quantity`.

In one Availability-local transaction:

1. lock reservation row
2. verify/resolve reservation state
3. lock exact inventory-date rows in deterministic date order
4. decrement `booked_quantity`
5. mark reservation `CANCELLED`
6. insert Availability outbox event if required
7. commit

DB constraints preserve:

```text
held_quantity >= 0
booked_quantity >= 0
held_quantity + booked_quantity <= total_quantity
```

---

## 14. CancelBookedReservation idempotency

A successful cancellation response may be lost.

Example:

```text
Booking -> Availability: CancelBookedReservation
Availability commits CANCELLED and releases booked inventory
response is lost
```

Retrying the command must return the already-cancelled reservation as success and must not decrement inventory again.

Therefore:

```text
BOOKED -> CANCELLED = perform side effect once
CANCELLED -> CANCELLED = idempotent success
```

Invalid source states such as HELD/EXPIRED/RELEASED should return a structured business error appropriate to the request.

---

## 15. Cancellation ordering: release inventory before refund

Two possible orders exist:

```text
refund -> release inventory
```

or:

```text
release inventory -> refund
```

The chosen design is:

```text
policy approved
    ↓
CancelBookedReservation
    ↓
Booking becomes CANCELLED
    ↓
process refund durably
```

Reasoning:

- A payment provider outage must not keep an accepted cancellation consuming room inventory.
- Once the business cancellation is accepted and inventory is returned, refund can remain PROCESSING/UNKNOWN and be recovered independently.
- Financial ambiguity is already handled through idempotent refund reconciliation.

---

## 16. Booking cancellation completion vs refund completion

Booking cancellation and refund completion are separate milestones.

Valid state example:

```text
Booking = CANCELLED
Refund  = PROCESSING
```

or:

```text
Booking = CANCELLED
Refund  = UNKNOWN
```

Do not create booking-status explosion such as:

```text
CANCELLED_REFUND_PENDING
CANCELLED_REFUND_UNKNOWN
CANCELLED_REFUND_FAILED
```

Refund lifecycle belongs to Payment/cancellation workflow state.

---

## 17. Local Booking transaction after inventory cancellation

After Availability confirms booked reservation cancellation, Booking commits locally:

```text
BEGIN

booking.status = CANCELLED
cancellation.state = RESERVATION_CANCELLED
INSERT BookingCancelled into outbox

COMMIT
```

`BookingCancelled` becomes durable as soon as business cancellation and inventory return are durable. It does not wait for the external payment provider to finish refund processing.

---

## 18. Zero-refund cancellation

If policy evaluation determines:

```text
refund_amount = 0
```

then no Payment refund command is necessary.

Cancellation can move from:

```text
RESERVATION_CANCELLED
    ↓
COMPLETED
```

while preserving the calculated zero-refund outcome for auditability.

---

## 19. Refund is a first-class Payment aggregate

A single `Payment.status = REFUNDED` field is too weak for durable refund behavior.

Payment Service should own:

```text
payments
refunds
refund_attempts
```

Recommended refund fields:

```text
refunds
-------
id
payment_id
booking_id
idempotency_key
amount
currency
status
provider_refund_id
failure_code
failure_message
created_at
updated_at
```

Refund statuses:

```text
PENDING
PROCESSING
SUCCEEDED
FAILED
UNKNOWN
```

This allows full/partial refund modeling and proper attempt history without overloading the original payment record.

---

## 20. Refund idempotency

Recommended logical identity:

```text
refund:<booking_id>:<cancellation_id>
```

`refunds.idempotency_key` is UNIQUE.

Repeated `CreateRefund` with the same identity and same immutable parameters returns the same logical refund.

The same key with conflicting parameters is an idempotency conflict.

---

## 21. Payment refund API refinement

Recommended Payment capability:

```proto
service PaymentService {
  rpc CreatePayment(...);
  rpc GetPayment(...);
  rpc CreateRefund(...);
  rpc GetRefund(...);
}
```

`CreateRefund` and `GetRefund` make the refund lifecycle explicit and support ambiguous transport recovery.

Usecases depend on inner Payment/Refund repository/provider interfaces. Provider SDK calls remain infrastructure concerns.

---

## 22. Refund timeout is UNKNOWN

The same financial ambiguity rule used for charge applies to refund.

Example:

```text
Payment -> Provider: refund
Provider succeeds
response is lost
```

The caller must not interpret the timeout as a confirmed refund failure and issue an independent second refund.

Correct state:

```text
REFUND_UNKNOWN
```

Recovery reconciles with authoritative Payment/provider state using `GetRefund` or the logical refund identity.

---

## 23. Cancellation recovery worker

Customer cancellation has its own recovery usecase:

```text
Application worker
    ↓
RecoverCancellationUsecase
    ↓
Booking / Availability / Payment repository interfaces
```

Do not put cancellation decisions into the worker loop itself.

Typical recovery:

```text
CANCELLING_RESERVATION
    ↓
retry idempotent CancelBookedReservation

RESERVATION_CANCELLED
    ↓
start/resume required refund

REFUND_PROCESSING / REFUND_UNKNOWN
    ↓
GetRefund / reconcile

REFUND_SUCCEEDED
    ↓
COMPLETED
```

Retry scheduling uses persisted `retry_count` and `next_retry_at`.

---

## 24. Crash recovery: inventory cancelled but Booking did not persist result

Availability:

```text
reservation = CANCELLED
```

Booking:

```text
booking = CONFIRMED
cancellation = CANCELLING_RESERVATION
```

Recovery repeats `CancelBookedReservation`.

Availability returns idempotent success for the existing CANCELLED reservation. Booking then persists `booking = CANCELLED` and continues.

---

## 25. Crash recovery: refund succeeded but response/result was lost

Payment:

```text
refund = SUCCEEDED
```

Booking cancellation:

```text
REFUND_PROCESSING or REFUND_UNKNOWN
```

Recovery calls `GetRefund` and obtains SUCCEEDED, then finalizes the cancellation operation.

No second refund is created.

---

## 26. HTTP cancellation idempotency

External cancellation uses `Idempotency-Key`.

Booking persists:

```text
idempotency_key
request_hash
cancellation_id
```

Semantics:

```text
same key + same request
    -> existing/current cancellation result

same key + different request
    -> IDEMPOTENCY_CONFLICT
```

The Gateway passes the key unchanged. Booking owns the business idempotency invariant.

---

## 27. Concurrent cancellation requests with different keys

Two clients/retries may issue different idempotency keys for the same booking concurrently.

Booking must prevent two active cancellation workflows.

Recommended MVP pattern:

```sql
SELECT booking
FOR UPDATE;
```

Inside the short Booking-local transaction:

1. re-check booking status
2. return existing cancellation result if already cancelled
3. detect an active cancellation operation
4. create at most one active cancellation workflow
5. persist fixed policy evaluation/refund amount
6. commit

A partial unique index on active cancellation rows can provide a second database-level defense.

---

## 28. Cancellation policy decision is fixed once

Cancellation captures:

```text
policy_evaluated_at
refund_amount
currency
```

before remote operations begin.

A request accepted before a free-cancellation deadline does not become non-refundable because Availability or Payment processing crosses the deadline later.

Retries use the persisted decision and do not re-run current pricing/policy logic.

---

## 29. Cancel vs CreateBooking races

MVP cancellation accepts only CONFIRMED bookings.

This intentionally avoids complex public cancellation races with:

```text
PAYMENT_PROCESSING
PAYMENT_UNKNOWN
CONFIRMING_RESERVATION
```

CreateBooking Saga owns those states until confirmation or failure.

If future product requirements need customer abort before confirmation, design it as a separate workflow rather than expanding CancelBooking implicitly.

---

## 30. Integration events

Two distinct business milestones should be emitted.

### BookingCancelled

Produced after the Booking local transaction commits the cancellation and inventory return is durable.

Suggested information:

```text
booking_id
user_id
cancelled_at
reason
refund_amount
currency
refund_status = PENDING | NOT_REQUIRED
```

### BookingRefundSucceeded

Produced after refund success is durably observed by Booking/cancellation workflow if consumers need a Booking-context event.

Notification can therefore truthfully send separate messages:

```text
Your booking has been cancelled. Your refund is being processed.
```

and later:

```text
Your refund has completed.
```

Cross-topic ordering between Booking and Payment events must never be assumed globally.

---

## 31. Kafka and outbox implications

Booking cancellation state changes and Booking integration events use the existing Transactional Outbox rules.

Availability may emit `ReservationCancelled` through its own outbox in the same transaction that returns booked inventory.

Payment emits refund lifecycle events through its own outbox if required.

All consumers remain at-least-once/idempotent according to `docs/kafka-event-architecture.md`.

---

## 32. Clean Architecture boundaries

### Booking

```text
Application HTTP/gRPC handler or worker
        ↓
CancelBookingUsecase / RecoverCancellationUsecase
        ↓
BookingRepository
CancellationRepository
AvailabilityRepository
PaymentRepository
OutboxRepository
```

### Availability

```text
Application gRPC handler
        ↓
CancelBookedReservationUsecase
        ↓
Reservation/Inventory Repository interfaces
        ↓
PostgreSQL adapter
```

### Payment

```text
Application gRPC handler
        ↓
CreateRefundUsecase / GetRefundUsecase
        ↓
PaymentRepository
RefundRepository
PaymentProvider
        ↓
PostgreSQL / provider adapters
```

No service queries another service's database.

---

## 33. Required integration/failure tests

Implementation must cover at least:

1. successful fully refundable cancellation
2. successful zero-refund cancellation
3. policy rejection while booking remains CONFIRMED
4. duplicate HTTP request with same idempotency key
5. concurrent different cancellation keys for same booking
6. lost Availability cancellation response and idempotent retry
7. cancellation worker restart after Availability success
8. refund provider timeout becomes UNKNOWN
9. lost refund response reconciles to SUCCEEDED
10. duplicate refund command cannot return money twice
11. booked inventory is decremented exactly once
12. `BookingCancelled` outbox entry is atomic with Booking cancellation state
13. deadline/policy evaluation crossing does not change persisted refund decision

---

## 34. Architectural decisions

The following decisions are baseline unless superseded by an explicit ADR:

1. Customer cancellation is a separate workflow from CreateBooking compensation.
2. MVP allows public cancellation only for CONFIRMED bookings.
3. Cancellation execution state is separate from customer-facing Booking status.
4. Accepted cancellation policy is produced with the exact Pricing Quote and snapshotted by Booking.
5. Booking enforces the accepted policy snapshot and does not re-query current Pricing policy during cancellation.
6. Human hotel-local cancellation rules are resolved into absolute instants before being snapshotted.
7. The cancellation policy evaluation instant and refund amount are persisted once before remote actions.
8. Availability uses a distinct `CancelBookedReservation` command and `BOOKED -> CANCELLED` state transition.
9. Booked cancellation decrements `booked_quantity`, never `held_quantity`.
10. `CancelBookedReservation` is idempotent.
11. Inventory cancellation happens before refund processing.
12. Booking may be CANCELLED while refund remains PROCESSING/UNKNOWN.
13. Refund is a first-class Payment aggregate with its own idempotency and attempts.
14. Refund timeout/lost response means UNKNOWN until reconciled.
15. Cancellation recovery is durable, retryable, and implemented in a dedicated usecase.
16. External cancellation idempotency is owned by Booking, not Gateway.
17. Only one active cancellation workflow may exist for a booking.
18. BookingCancelled and refund completion are separate durable milestones/events.

---

## 35. Implementation policy

Coding tickets implement these decisions. They must not redesign cancellation ordering, refund ambiguity semantics, accepted-policy ownership, Availability state transitions, or idempotency boundaries inside implementation PRs.

Any discovery requiring an architecture change must be raised separately and resolved in architecture/design before implementation diverges.
