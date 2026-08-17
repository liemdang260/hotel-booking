# Current Architecture Baseline and Decision Precedence

## Purpose

The hotel booking platform has been designed incrementally. Earlier design documents remain useful for context, but later specialized decisions intentionally refine or supersede parts of earlier drafts.

This document is the current architecture index and conflict-resolution guide for implementation agents and reviewers.

It does **not** replace the detailed design documents. It tells an implementer which documents are authoritative for each concern and what to do when older documents disagree with newer specialized decisions.

---

## 1. Decision precedence

When requirements appear inconsistent, use this precedence:

```text
1. Current Jira ticket acceptance criteria / architecture constraints
2. Latest specialized architecture document for that concern
3. Current architecture baseline (this document)
4. Older broad architecture/overview documents
5. Existing implementation if it conflicts with an approved architecture decision
```

Do not silently choose an older design because it is easier to implement.

If two current specialized documents genuinely conflict and Jira does not resolve the conflict, stop that implementation scope and raise the architecture conflict instead of inventing a new behavior in a coding PR.

---

## 2. Architecture source index

### Platform overview and initial decomposition

- PR #2 — initial architecture plan
- PR #3 — use cases

These are historical/high-level context. Later specialized designs win on detailed behavior.

### CreateBooking and failure scenarios

- PR #4 — initial CreateBooking workflow/failure design
- PR #13 — durable Booking/Payment Saga recovery and financial ambiguity
- PR #16 — customer-approved exact Quote refinement

PR #13 and PR #16 supersede older assumptions where CreateBooking appears to calculate/accept a fresh price immediately before payment.

Current public flow is:

```text
Search estimate
    ↓
Exact immutable Quote
    ↓
Customer accepts quote
    ↓
CreateBooking(quote_id)
    ↓
ReserveInventory
    ↓
Payment
    ↓
ConfirmReservation
    ↓
Booking CONFIRMED + outbox
```

### Data ownership and service databases

- PR #5 — data ownership and database schemas

Core rule:

> The service that owns a business invariant owns the data and local transaction protecting that invariant.

No cross-service database joins or foreign keys.

### Clean Architecture

- PR #9 — Clean Architecture conventions + Booking persistence design

Required dependency direction in every service:

```text
Application entry point
        ↓
Usecase
        ↓
Repository / gateway interfaces
        ↓
Infrastructure adapters
```

Application contains entry points only: HTTP/gRPC handlers, Kafka consumer entry points and scheduled/worker entry points.

Usecases own business orchestration/transaction boundaries and must not depend directly on protobuf-generated clients, pgx, Kafka clients, Redis clients, HTTP frameworks, payment provider SDKs, etc.

### Availability correctness/concurrency

- PR #7 — Availability concurrency/reservation lifecycle design
- PR #17 — booked-reservation cancellation extension

Final inventory model is:

```text
room_inventory(
  hotel_id,
  room_type_id,
  inventory_date,
  total_quantity,
  held_quantity,
  booked_quantity,
  version,
  timestamps
)

available = total_quantity - held_quantity - booked_quantity
```

A single `reserved_quantity` model is obsolete and must not be introduced.

Transitions:

```text
ReserveInventory:
  held_quantity += q

ConfirmReservation:
  held_quantity -= q
  booked_quantity += q

ReleaseReservation / Expire:
  held_quantity -= q

CancelBookedReservation:
  booked_quantity -= q
```

Reservation states:

```text
HELD -> BOOKED
HELD -> RELEASED
HELD -> EXPIRED
BOOKED -> CANCELLED
```

PostgreSQL is authoritative. Redis/distributed locks are not used for inventory correctness initially.

### Pricing / accepted commercial offer

- PR #16 — exact customer-approved Quote flow
- PR #17 — cancellation policy as accepted commercial term

Search pricing is advisory only.

Exact Quote is immutable and has a TTL. CreateBooking uses `quote_id`, never a client-controlled amount.

The accepted Quote contains:

```text
stay/room/guest inputs
price breakdown + total + currency
pricing version
expiry
accepted cancellation terms
```

Booking snapshots accepted price and cancellation policy before downstream side effects.

### Payment and financial ambiguity

- PR #13 — charge identity, UNKNOWN state and reconciliation
- PR #17 — first-class refund aggregate and cancellation refund flow

Current payment states include:

```text
PENDING
PROCESSING
SUCCEEDED
FAILED
UNKNOWN
```

A timeout/lost response is not a confirmed financial failure.

```text
ambiguous charge -> PAYMENT_UNKNOWN -> reconcile
ambiguous refund -> REFUND_UNKNOWN -> reconcile
```

Do not blindly issue a new financial operation because the response timed out.

Refund is a first-class aggregate (`refunds`, `refund_attempts`, CreateRefund/GetRefund) rather than overloading the original Payment status with `REFUNDED` as the only representation.

### Booking Saga and recovery

- PR #13 — durable Saga orchestration/recovery

Booking Service is the Saga orchestrator.

Required pattern:

```text
remote idempotent/reconcilable action
    ↓
persist durable local result
    ↓
next remote action
```

Never hold a long Booking DB transaction across network calls.

Recovery resumes persisted state after process crashes.

Downstream mutation commands must be idempotent.

Compensation is durable/retryable, not a best-effort `defer`.

### Customer cancellation

- PR #17 — cancellation/refund architecture

Customer cancellation is a separate workflow from CreateBooking compensation.

MVP:

```text
only CONFIRMED booking can enter public cancellation
```

Order:

```text
persist policy decision/refund amount once
    ↓
CancelBookedReservation
    ↓
Booking = CANCELLED + BookingCancelled outbox
    ↓
process/refine refund durably
```

Booking may be CANCELLED while refund remains PROCESSING/UNKNOWN.

### Kafka / integration events

- PR #14 — Kafka event architecture/delivery guarantees

Kafka does not orchestrate the correctness-critical Booking Saga.

Core Booking interactions remain synchronous gRPC for Pricing/Availability/Payment.

Kafka semantics:

```text
at-least-once delivery
Transactional Outbox for producer DB->Kafka boundary
Inbox/processed_events for consumer dedupe
idempotent consumers
aggregate-based partition keys
versioned integration events
```

Duplicate publication/delivery is expected behavior.

No global event ordering is assumed across topics/services.

### Gateway / Auth / Catalog / Search

- PR #16 — Gateway, Auth, Catalog and Search/query boundaries
- PR #19 — security/trust boundaries

Gateway is the only customer-facing boundary.

Auth:

```text
short-lived asymmetric JWT access token
opaque hashed refresh token with rotation/revocation
Gateway verifies JWT locally
```

Catalog owns hotel/room-type metadata only.

Availability owns inventory; Pricing owns rate/commercial terms.

MVP search:

```text
bounded Catalog candidates
    ↓
batch Availability read
    ↓
batch Pricing estimate
    ↓
Gateway composition
```

Do not introduce Search Service/OpenSearch until measured requirements justify it.

### Security / trust boundaries

- PR #19 — security and service trust boundaries

Key rules:

```text
public client headers are untrusted
Gateway strips/overwrites internal identity metadata
Gateway authenticates customer
owning service authorizes resource
end-user identity != calling-service identity
```

Production target uses workload identity/mTLS or equivalent for internal service authentication; private network placement alone is not identity.

Raw payment credentials, bearer/refresh tokens and provider secrets must never appear in logs/traces/Kafka.

Payment data model uses provider/tokenized references rather than raw card credentials.

### Reliability / production operability

- PR #18 — production reliability and operability

Priority:

```text
correctness/invariants
    > durable recoverability
    > bounded blast radius
    > accurate outcome
    > latency/availability optimization
```

Retry is operation-specific and has an explicit owner to avoid retry amplification.

Context/deadline propagates end-to-end.

Liveness and readiness are different; Kafka/downstream failure does not automatically make Booking unready because Outbox/recovery decouple those concerns.

Workers, DB pools and in-memory queues are bounded.

Production migration lifecycle follows expand -> optional backfill -> contract.

---

## 3. Current end-to-end architecture

```text
                              Internet Client
                                    │
                              HTTPS / REST
                                    │
                                    ▼
                              API Gateway
                     AuthN / limits / public mapping
                                    │
          ┌─────────────────────────┼───────────────────────────┐
          │                         │                           │
          ▼                         ▼                           ▼
        Auth                      Catalog                    Booking
                                      │                         │
                                      │            ┌────────────┼────────────┐
                                      │            │            │            │
                                      ▼            ▼            ▼            ▼
                                  metadata       Pricing   Availability    Payment
                                                               │            │
                                                          inventory DB   provider

Booking / Availability / Payment
             │
     local domain transaction
             │
           Outbox
             │
             ▼
           Kafka
             │
       ┌─────┼─────┐
       ▼     ▼     ▼
 Notification  Audit  Analytics
```

Each service owns its own data and local transaction boundaries.

---

## 4. Authoritative write paths

### Inventory

```text
PostgreSQL Availability DB
```

not Redis/search cache.

### Booking

```text
Booking DB + durable Saga state
```

### Price charged

```text
customer-approved Quote
    ↓
Booking immutable price snapshot
```

### Payment/refund truth

```text
Payment DB/provider reconciliation
```

### Events

```text
service DB transaction + Transactional Outbox
```

Kafka is transport, not the authoritative source of the transactional write.

---

## 5. Important non-goals for MVP

Do not add these unless a later design/metric explicitly requires them:

```text
Redis distributed lock for room booking
Kafka-driven choreography for core booking transaction
Search Service / Elasticsearch/OpenSearch
shared cross-service database
cross-service foreign keys
exactly-once business-delivery claims
public Payment/Availability endpoints
arbitrary distributed transaction / 2PC
blind financial retries
large generic shared domain library
```

The portfolio value comes from correctness/recovery/ownership clarity, not maximizing service count or infrastructure complexity.

---

## 6. Implementation-agent rules

Before coding a Jira task:

1. Read the full Jira description/acceptance criteria/comments/architecture links.
2. Read the latest specialized design referenced by that ticket.
3. Use `seq-NNN` as Jira implementation order; Jira Blocks links are known to contain historical inconsistent two-way links and are not authoritative.
4. Treat an earlier task in In Review as sufficient implementation progress to begin its immediate successor during this early project phase.
5. Implement the architecture; do not redesign it inside the coding PR.
6. If code/repository state conflicts with a current architecture decision, identify the conflict in the PR instead of silently preserving stale behavior.
7. Never create duplicate side effects to compensate for transport uncertainty; follow idempotency/reconciliation rules.
8. Keep Application entry points thin and keep infrastructure imports out of usecase/domain.

---

## 7. Known superseded concepts

The following concepts may appear in older docs/comments and must be treated as obsolete when encountered:

### `reserved_quantity`

Obsolete. Final Availability model uses `held_quantity + booked_quantity`.

### CreateBooking accepting an authoritative amount or silently re-quoting immediately before charge

Obsolete. Customer-approved exact `quote_id` is authoritative and Booking snapshots it.

### Payment timeout means failure

Obsolete. Ambiguous outcome is UNKNOWN until reconciled.

### Refund represented only by `Payment.status = REFUNDED`

Obsolete for the final cancellation model. Refund is first-class.

### `ReleaseReservation` used for confirmed booking cancellation

Obsolete. HELD release and BOOKED cancellation are distinct transitions.

### Kafka orchestrates Reserve -> Pay -> Confirm

Not the MVP architecture. Booking Service orchestrates synchronously with durable Saga recovery.

### Gateway alone authorizes booking ownership

Incorrect. Gateway authenticates; Booking enforces object/resource authorization.

### Internal network means trusted caller

Only a local-development simplification. Production target uses authenticated workload/service identity.

---

## 8. Backlog baseline

Jira implementation sequencing is authoritative through `seq-NNN` labels.

Current milestone boundaries conceptually are:

```text
M1 Foundation
M2 Availability correctness
M3 Booking Saga core
M4 Pricing / Payment / Cancellation reliability
M5 Auth / Catalog / Gateway / Events / E2E
M6 Observability / reliability / security / deployment hardening
```

Architecture work may discover a real missing implementation boundary. PM/BA may insert a ticket and re-sequence downstream labels; coding agents must always re-read Jira rather than hard-code old issue numbers/order.

---

## 9. Architecture change process

If implementation reveals that an approved decision is infeasible or materially harmful:

```text
implementation discovery
    ↓
raise explicit architecture issue/comment
    ↓
Architect reviews invariant/tradeoff
    ↓
design/ADR updated
    ↓
Jira acceptance criteria updated
    ↓
implementation proceeds
```

Do not bury architecture changes in a coding diff.

---

## 10. Baseline status

This baseline reflects the architecture after the specialized designs through:

```text
Booking/Payment recovery
Kafka delivery
Gateway/Auth/Catalog/customer-approved Quote
Customer cancellation/refund
Production reliability/operability
Security/trust boundaries
```

Further architecture work should update this baseline when it changes an implementation-visible decision.
