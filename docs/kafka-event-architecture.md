# Kafka and Event Architecture

## Status

Architecture baseline for asynchronous integration in the hotel booking platform.

This document defines how domain state changes leave a service boundary, how events are published reliably, how consumers process at-least-once delivery safely, and how ordering, versioning, retries, DLQ handling, and Clean Architecture boundaries are enforced.

## 1. Core decision

Kafka is **not** the orchestrator of the core Booking Saga.

The synchronous booking path remains:

```text
Client
  -> Gateway
  -> Booking Service
       -> Pricing Service (gRPC)
       -> Availability Service (gRPC)
       -> Payment Service (gRPC)
```

Kafka is used after durable business changes for asynchronous integration and side effects:

```text
Service transaction
  -> local Outbox
  -> Outbox Publisher
  -> Kafka
  -> Notification / Audit / Analytics / projections
```

This keeps booking correctness in explicit request/response commands while retaining event-driven extensibility for non-blocking workflows.

## 2. Delivery model

The system is designed for **at-least-once delivery**.

Duplicate event delivery is expected and is not treated as an infrastructure anomaly. Consumers must be idempotent.

The architecture does not claim end-to-end exactly-once business processing across PostgreSQL, Kafka, and external providers.

## 3. Common event envelope

All integration events use a common logical envelope:

```json
{
  "event_id": "0198...",
  "event_type": "BookingConfirmed",
  "event_version": 1,
  "aggregate_type": "booking",
  "aggregate_id": "booking_123",
  "aggregate_version": 7,
  "occurred_at": "2026-08-09T16:00:00Z",
  "correlation_id": "req_123",
  "causation_id": "cmd_or_event_456",
  "payload": {}
}
```

### event_id

Unique identity of the integration event. Prefer UUIDv7 when practical, but consumers treat the value as opaque. It is the primary deduplication key.

### event_type and event_version

`event_type` is a stable semantic name such as `BookingConfirmed`, `ReservationExpired`, or `PaymentRefunded`.

`event_version` is the schema/contract version. Breaking changes require a new version. Existing versions must not silently change meaning.

### aggregate_id and aggregate_version

`aggregate_id` is used as the Kafka partition key so events for one aggregate remain ordered inside the same partition.

`aggregate_version` is a monotonic state version when applicable. Stateful consumers can use it to reject stale replayed state transitions.

### correlation_id and causation_id

`correlation_id` carries the logical workflow identity across synchronous and asynchronous boundaries.

`causation_id` identifies the command or previous event that produced this event.

## 4. Topic strategy

Prefer topics by bounded context rather than one topic per event type:

```text
booking.events.v1
availability.events.v1
payment.events.v1
```

Examples:

```text
booking.events.v1
  BookingConfirmed
  BookingCancelled
  BookingFailed

availability.events.v1
  ReservationExpired
  ReservationReleased

payment.events.v1
  PaymentSucceeded
  PaymentFailed
  PaymentRefunded
```

This avoids topic proliferation while retaining bounded-context ownership.

## 5. Partitioning and ordering

Kafka ordering guarantees only apply within one partition.

Partition keys:

```text
Booking events      -> booking_id
Availability events -> reservation_id
Payment events      -> booking_id
```

The system may assume ordering for events of the same aggregate key, but must never assume global ordering across partitions or topics.

A stateful consumer may keep the highest processed `aggregate_version`. If it has already observed Booking version 8 and later receives version 7, the consumer may ignore version 7 as stale when its projection semantics require monotonic state.

Notification-style consumers usually only need event-id deduplication.

## 6. Integration event versus domain entity

Do not serialize domain entities directly into Kafka.

```text
Domain action
  -> integration event mapping
  -> Outbox record
  -> Kafka
```

Integration-event contracts are versioned independently from internal domain models so entity refactors do not accidentally become cross-service breaking changes.

## 7. Serialization

Prefer Protobuf for Kafka integration-event contracts because the platform already uses Protobuf/gRPC.

Suggested structure:

```text
proto/events/booking/v1/
proto/events/availability/v1/
proto/events/payment/v1/
```

A schema registry is optional for the MVP and can be added later if operational value justifies it.

## 8. Transactional Outbox

Direct DB-then-Kafka publication is forbidden for durable domain changes.

Incorrect:

```text
COMMIT booking = CONFIRMED
then Kafka.Publish()
```

If Kafka fails after the commit, durable state exists without the required event.

Correct:

```text
BEGIN
  update aggregate state
  insert outbox event
COMMIT
```

An independent publisher later moves the event from PostgreSQL to Kafka.

Recommended logical outbox schema:

```sql
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    aggregate_type VARCHAR(50) NOT NULL,
    aggregate_id UUID NOT NULL,
    aggregate_version BIGINT,
    event_type VARCHAR(100) NOT NULL,
    event_version INTEGER NOT NULL,
    payload JSONB NOT NULL,
    correlation_id VARCHAR(100),
    causation_id VARCHAR(100),
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ,
    locked_by VARCHAR(100),
    locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);
```

Recommended queue index:

```sql
CREATE INDEX idx_outbox_pending
ON outbox_events(status, next_attempt_at, created_at);
```

If aggregate identifiers are not UUIDs in a service, store them as opaque strings instead.

## 9. Outbox publisher execution model

Do not hold a DB transaction open while calling Kafka.

Incorrect:

```text
BEGIN
  SELECT ... FOR UPDATE
  Kafka.Publish()
  UPDATE published
COMMIT
```

Preferred:

```text
short DB transaction: claim records
  -> commit
publish Kafka outside transaction
  -> short DB transaction: mark published or schedule retry
```

Multiple service instances may publish concurrently. Claim work with a short `FOR UPDATE SKIP LOCKED` transaction:

```sql
SELECT id
FROM outbox_events
WHERE status = 'PENDING'
  AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
ORDER BY created_at
LIMIT 100
FOR UPDATE SKIP LOCKED;
```

Claimed records transition to `PROCESSING` and receive a bounded lease via `locked_by` and `locked_until`.

## 10. Duplicate publication is expected

Failure scenario:

```text
Kafka publish succeeds
process crashes before outbox row is marked PUBLISHED
lease expires
publisher retries
same logical event is published again
```

This is valid at-least-once behavior. Consumers must deduplicate.

## 11. Outbox retry policy

Temporary publish failure returns the row to a retryable state:

```text
attempt_count += 1
next_attempt_at = backoff
```

Example backoff:

```text
1s -> 5s -> 30s -> 2m -> 10m
```

After an operational threshold, rows may transition to `FAILED` for alerting/manual recovery. Failed rows are retained.

Recommended metrics:

```text
outbox_pending_count
outbox_publish_failures_total
outbox_oldest_pending_seconds
outbox_publish_latency_seconds
```

## 12. Clean Architecture for producers

```text
Application worker
  -> PublishOutboxUsecase
       -> OutboxRepository interface
       -> EventPublisher interface
            -> Infrastructure PostgreSQL/Kafka adapters
```

The usecase layer must not import Kafka client types.

Example interface:

```go
type EventPublisher interface {
    Publish(ctx context.Context, event domain.IntegrationEvent) error
}
```

Kafka configuration, serializers, producers, headers, and client-specific types remain in Infrastructure.

## 13. Consumer architecture

Kafka consumers are Application entry points, like HTTP/gRPC handlers and scheduled workers.

```text
Kafka message
  -> Application consumer handler
  -> Usecase
  -> repository/provider interfaces
  -> Infrastructure
```

The Application consumer may deserialize and validate the event, map it to usecase input, and translate the outcome into commit/retry/DLQ behavior. It must not contain business persistence logic or direct DB calls.

## 14. Consumer idempotency / Inbox

Consumers that persist local state must deduplicate messages.

```sql
CREATE TABLE processed_events (
    event_id UUID PRIMARY KEY,
    event_type VARCHAR(100) NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

A consumer-local transaction atomically records the event identity with the local effect:

```text
BEGIN
  insert processed_events(event_id)
  apply local state change
COMMIT
then commit Kafka offset
```

A duplicate `event_id` means the local effect was already committed and the message can be acknowledged safely.

## 15. External side effects require a second durable boundary

Sending email/SMS directly in the Kafka consumer creates another dual-write problem.

Notification Service should convert the incoming event into a durable local notification job:

```text
Kafka consumer transaction
  -> insert processed_events(event_id)
  -> insert notifications(status=PENDING)
COMMIT
commit Kafka offset

Notification worker
  -> claim PENDING notification
  -> Email/SMS provider
  -> mark SENT or schedule retry
```

Use `notifications.event_id UNIQUE` so duplicate Kafka delivery cannot create a second logical notification job.

## 16. Retry classification

Failures are classified rather than blindly retried.

### Transient

Examples: temporary DB outage, temporary dependency outage, broker/network interruption. Retry with bounded backoff.

### Permanent/business-invalid

Examples: unsupported event version, malformed required fields, impossible contract values. Do not hot-loop indefinitely; route to DLQ after classification.

### Poison message

A deterministic repeatedly failing message must be isolated rather than blocking a partition forever.

## 17. Retry topics and DLQ

The MVP may start with bounded local retries followed by DLQ.

A later hardening phase may add staged retry topics:

```text
booking.events.v1
  -> booking.events.retry.10s
  -> booking.events.retry.1m
  -> booking.events.retry.10m
  -> booking.events.dlq
```

Retry topics are intentionally deferred until operational complexity is justified.

A DLQ record should preserve original broker context plus failure metadata:

```json
{
  "original_topic": "booking.events.v1",
  "original_partition": 3,
  "original_offset": 9281,
  "failure_code": "UNSUPPORTED_EVENT_VERSION",
  "failure_message": "...",
  "failed_at": "...",
  "consumer": "notification-service",
  "original_event": {}
}
```

## 18. Schema evolution

Never change the meaning of an existing event version silently.

Consumers explicitly handle supported versions:

```go
switch event.Version {
case 1:
    return handleV1(event)
case 2:
    return handleV2(event)
default:
    return ErrUnsupportedEventVersion
}
```

Breaking changes create a new versioned contract.

## 19. Event payload design

Events contain consumer-relevant snapshots, not complete service entities.

`BookingConfirmedV1` may contain:

```text
booking_id
user_id
recipient/email snapshot when deliberately chosen
hotel_id
room_type_id
check_in
check_out
total_amount
currency
```

It should not contain implementation details such as Saga state, retry count, internal errors, or repository timestamps.

Consumers must never query another service's database directly. If additional data is needed, include an intentional snapshot or call the owning service's public API.

## 20. Observability and tracing

The envelope carries `correlation_id`; Kafka headers propagate W3C tracing metadata such as `traceparent` and `tracestate`.

Desired trace shape:

```text
REST Gateway
  -> gRPC Booking
  -> DB transaction / outbox insert
  -> Kafka producer span
  -> Kafka consumer span
  -> Notification job creation
```

Async tracing must not make domain/usecase packages depend directly on OpenTelemetry implementation APIs.

Recommended metrics:

```text
kafka_produce_total
kafka_produce_errors_total
kafka_consume_total
kafka_consume_errors_total
consumer_lag
consumer_processing_latency_seconds
dlq_total
processed_event_duplicates_total
```

Avoid high-cardinality labels such as booking IDs or event IDs.

## 21. Business-event invariants

### Booking confirmation

When Booking reaches `CONFIRMED`, the local transaction also persists `BookingConfirmed`:

```text
BEGIN
  bookings.status = CONFIRMED
  booking_sagas.state = COMPLETED
  insert BookingConfirmed outbox event
COMMIT
```

### Reservation expiration

Availability follows the same rule:

```text
BEGIN
  release held inventory
  reservation.status = EXPIRED
  insert ReservationExpired outbox event
COMMIT
```

## 22. Failure scenarios to test

1. Business transaction commits while Kafka is unavailable.
2. Kafka publish succeeds but publisher crashes before marking the outbox row published.
3. Two outbox workers attempt to claim work concurrently.
4. Kafka delivers the same event multiple times.
5. Consumer local transaction succeeds but offset commit is lost.
6. Consumer receives an unsupported event version.
7. Consumer receives a poison event that repeatedly fails processing.
8. Notification provider fails after the Kafka event has been safely consumed.
9. Stale aggregate version arrives after a newer state-projection event.
10. Trace/correlation metadata crosses producer and consumer boundaries.

## 23. Architecture decisions

1. Kafka does not orchestrate the core Booking Saga.
2. End-to-end delivery semantics are at least once.
3. Consumers are idempotent by design.
4. Transactional Outbox solves service-DB to Kafka dual writes.
5. Processed-event/Inbox persistence handles duplicate consumption.
6. External consumer side effects use a second local durable job boundary.
7. Kafka partition key is derived from aggregate identity.
8. Ordering assumptions are limited to one aggregate/partition.
9. Integration events include `event_id` and optionally `aggregate_version`.
10. Duplicate producer publication is expected after crash recovery.
11. Application Kafka handlers/workers are entry points only.
12. Usecases depend on repository/publisher interfaces, not Kafka clients.
13. Integration-event contracts are versioned separately from domain entities.
14. Protobuf is the preferred event serialization format.
15. Trace context is propagated through Kafka headers.
16. DLQ is reserved for permanent/poison failures; transient failures receive bounded retry.
17. Retry topics are a later operational-hardening feature, not an MVP requirement.

## 24. Implementation policy

Implementation tickets must follow these decisions rather than redesigning them inside coding PRs. Architectural deviations should be raised separately with rationale and failure-mode analysis.
