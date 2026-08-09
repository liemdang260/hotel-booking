# Production Reliability and Operability Design

## Purpose

This document defines the platform-wide reliability and operability rules for the hotel booking system. The goal is not to maximize availability at any cost. For correctness-critical operations such as inventory reservation, payment, refund, and booking state transitions, preserving business invariants is more important than returning a successful response when the system does not know the truth.

The design applies to API Gateway, Booking, Availability, Pricing, Payment, Catalog, Auth, Notification, Kafka workers, Saga recovery workers, outbox publishers, and future services unless a service-specific ADR overrides it.

Clean Architecture remains mandatory:

```text
Application entry point
        ↓
Usecase
        ↓
Repository / gateway interfaces
        ↓
Infrastructure adapters
```

Transport, retry middleware, metrics, tracing, database libraries, Kafka clients, circuit breakers, and provider SDKs must remain outside core business logic unless a business workflow explicitly needs to react to a classified outcome.

---

## 1. Reliability priority

The system uses the following priority order:

```text
1. Preserve business correctness/invariants
2. Preserve durable recoverability
3. Bound resource usage and blast radius
4. Return an accurate outcome to the caller
5. Optimize latency/availability
```

Examples:

- A Payment timeout must become UNKNOWN/reconciliation rather than a guessed failure.
- A sold-out room must return a business conflict rather than oversell because a cache says capacity exists.
- Kafka unavailability must not roll back an already-valid booking transaction because Transactional Outbox decouples event publication.

---

## 2. Classify operations before choosing retry behavior

Every remote or infrastructure operation belongs to one of three broad classes.

### Class A: read-only / side-effect-free

Examples:

```text
Catalog.GetHotel
Catalog.Search
Availability.CheckAvailability
Pricing estimate reads
GetPayment
GetRefund
GetBooking
```

Transient transport retry is generally safe within a bounded deadline.

### Class B: idempotent mutation with explicit business identity

Examples:

```text
ReserveInventory(booking_id)
ConfirmReservation(reservation_id)
ReleaseReservation(reservation_id)
CancelBookedReservation(reservation_id)
CreatePayment(payment idempotency identity)
CreateRefund(refund idempotency identity)
```

Retries are allowed only because the command is designed to be idempotent and protected by a durable database invariant.

### Class C: ambiguous financial/provider side effect

Examples:

```text
provider Charge
provider Refund
```

A timeout/lost response is not automatically retryable at the provider-operation level. It becomes an ambiguous outcome that must be reconciled using the same logical identity and provider truth.

This distinction must be documented in adapters/usecases and tests.

---

## 3. One owner for retries to prevent retry amplification

A call path may include:

```text
Gateway
  ↓
Booking
  ↓
Payment
  ↓
Provider
```

If every layer retries three times, a single request can become dozens of downstream attempts.

Rule:

> Retry ownership is explicit. Do not independently configure aggressive automatic retries at every layer.

Recommended pattern:

- Gateway: no mutation retry; optionally one very small retry for safe reads if useful.
- Booking usecase/adapters: owns bounded retry for idempotent downstream business commands when workflow semantics require it.
- Payment Service: owns provider retry/reconciliation policy, not Booking.
- Database drivers: connection establishment may have driver-level retry behavior, but transaction statements are not blindly replayed by infrastructure without usecase/domain awareness.
- Kafka clients: library retries may be used for producer transport delivery, but outbox state remains the durable source of retry/republication.

---

## 4. Deadline propagation

Every synchronous inbound request has one upstream context/deadline.

Required propagation:

```text
HTTP request context
        ↓
Gateway usecase
        ↓
gRPC client context
        ↓
service Application handler
        ↓
usecase
        ↓
repository / DB / provider adapter
```

Never replace the request context with `context.Background()` in a synchronous request path.

A child call may use a shorter timeout budget but never exceed the remaining upstream deadline.

Conceptual helper:

```go
func ChildTimeout(ctx context.Context, desired time.Duration) (context.Context, context.CancelFunc)
```

The helper should clamp the child timeout to the remaining parent deadline.

---

## 5. Deadline budget by operation type

Exact values are configuration/tuning parameters, but the architecture sets relative expectations.

Example initial budgets:

```text
Gateway search request        ~ 2-3s total
Catalog batch/read            ~ 300-500ms
Availability read             ~ 300-500ms
Pricing quote/read            ~ 500-800ms
ReserveInventory              ~ 1s
Confirm/Release/Cancel        ~ 1s
Payment Service call          ~ 1.5-2s
Provider charge/refund        bounded by Payment policy
```

The system must not hard-code these values across business packages. Central typed configuration provides defaults by adapter/operation.

Performance testing later tunes the values.

---

## 6. Timeout semantics are operation-specific

The same infrastructure timeout has different business meaning depending on the operation.

Examples:

```text
Pricing timeout
    -> safe retry or SERVICE_UNAVAILABLE

ReserveInventory timeout
    -> retry same idempotent booking identity / recover existing reservation

Payment provider timeout
    -> PAYMENT_UNKNOWN and reconciliation

Refund provider timeout
    -> REFUND_UNKNOWN and reconciliation

Kafka publish timeout
    -> outbox remains retryable; business transaction is not undone
```

Do not map all DeadlineExceeded errors to one generic business transition.

---

## 7. Retry backoff and jitter

Retry loops must be bounded.

Recommended infrastructure/business worker pattern:

```text
attempt 1
  ↓
small randomized delay
  ↓
attempt 2
  ↓
larger randomized delay
```

Durable recovery workers persist:

```text
retry_count
next_retry_at
```

and use exponential-style backoff with jitter.

The exact schedule is configurable. A simple MVP schedule such as 5s, 15s, 60s, 5m is acceptable for durable workflow retries.

No infinite tight retry loops.

---

## 8. Circuit breakers: use selectively

Circuit breakers are useful when a dependency is repeatedly failing and immediate retries waste resources.

They are not a substitute for business state/recovery.

Good candidates:

```text
external payment provider
email/SMS provider
possibly high-cost remote read dependency
```

Use cautiously for core internal commands such as Availability reservation because a breaker-open result still needs a precise Saga/retry interpretation.

Recommended behavior:

- infrastructure adapter tracks dependency failure rate/window
- breaker open returns a classified transient-unavailable error
- usecase maps that result into its existing retry/recovery semantics
- breaker state never becomes the source of business truth

---

## 9. Bulkheads and concurrency limits

The system must bound concurrent work even when traffic spikes.

Limits exist at multiple layers:

```text
Gateway request concurrency / rate limits
DB connection pool size
per-downstream gRPC concurrency where necessary
Saga recovery worker concurrency
expiration worker concurrency
outbox publisher batch/concurrency
Kafka consumer concurrency
notification provider concurrency
```

Do not allow one worker class to consume all DB connections and starve synchronous booking requests.

Worker pools should have independently configurable concurrency/batch limits.

---

## 10. Database connection pool as a hard capacity boundary

Each service owns a bounded PostgreSQL pool.

Rules:

- pool size is explicit/configured
- request handlers respect context cancellation while waiting for a connection
- worker concurrency is sized relative to remaining pool capacity
- pool exhaustion is observable with metrics
- increasing pool size is not the first fix for slow queries/lock contention

Useful metrics:

```text
db_pool_acquired
db_pool_idle
db_pool_wait_duration
db_query_duration
db_lock_wait_duration
```

Avoid high-cardinality query labels.

---

## 11. Backpressure instead of unbounded queues

Internal application queues/channels must be bounded.

When capacity is exhausted:

- synchronous requests should fail fast with a stable unavailable/overloaded response when appropriate
- durable asynchronous work should stay in PostgreSQL/Kafka rather than accumulate indefinitely in process memory
- workers should stop claiming more work until capacity returns

PostgreSQL outbox/Saga tables and Kafka are the durable queues. In-memory goroutine fan-out is an execution mechanism, not durable buffering.

---

## 12. Load shedding at Gateway

Gateway may use Redis-backed or local/distributed rate limiting for non-authoritative traffic protection.

Candidate limits:

```text
/login         per IP + account key
/search        high read allowance
/quotes        per user/IP
/bookings      per authenticated user
/cancel        per authenticated user
```

Rate limiting protects downstream resources but does not implement business correctness.

If rate-limit Redis is unavailable, failure behavior is a product/availability choice; Redis does not become booking source of truth.

---

## 13. Liveness and readiness are different

### Liveness

Answers:

> Is the process itself alive and able to make progress?

Liveness should not fail because a dependency is temporarily unavailable.

A database or Kafka outage should not cause Kubernetes to restart a healthy process repeatedly.

### Readiness

Answers:

> Should this instance receive new synchronous traffic now?

Readiness may check local critical ability to serve requests.

---

## 14. Readiness dependency policy

Do not make readiness a deep dependency graph health check.

Example Booking readiness:

Required:

```text
process initialized
configuration valid
Booking DB reachable / pool functional
server accepting requests
```

Not required for readiness:

```text
Kafka reachable
Payment provider reachable
every downstream gRPC service currently healthy
```

Why:

- Kafka is decoupled by outbox.
- external/downstream transient failures are handled per request/workflow.
- making readiness depend on every downstream can cause cascading fleet removal.

Service-specific exceptions require explicit ADR.

---

## 15. Worker health

Worker-based services expose health separate from synchronous API readiness where useful.

Metrics/alerts are more important than forcing process restarts for stuck business work.

Examples:

```text
saga_oldest_recoverable_age_seconds
outbox_oldest_pending_age_seconds
reservation_expiry_lag_seconds
notification_oldest_pending_age_seconds
```

A worker process can be alive but operationally unhealthy because backlog age is growing. Alert on lag/backlog, not only process uptime.

---

## 16. Graceful shutdown sequence

On SIGTERM or orchestrator termination:

```text
1. mark instance not ready
2. stop accepting new work / stop claiming new jobs
3. allow in-flight HTTP/gRPC requests to drain within shutdown deadline
4. allow current short DB transaction to finish/rollback
5. finish or safely abandon current idempotent worker operation
6. commit Kafka offsets only for completed processing
7. close listeners, clients and DB pools
8. exit before orchestrator kill deadline
```

No new worker claims after shutdown begins.

Long remote calls remain bounded by their context/deadline and must not delay shutdown indefinitely.

---

## 17. Graceful shutdown for Saga/outbox workers

A worker may die after a remote side effect but before marking local success.

This is expected and handled by idempotency/reconciliation.

Therefore shutdown does not need to guarantee every claimed remote action completes perfectly.

It must guarantee one of:

```text
result persisted
```

or

```text
claim lease/version eventually becomes recoverable by another worker
```

No permanent in-memory ownership.

---

## 18. Claim leases for durable workers

Where durable jobs use claim leasing, store fields such as:

```text
locked_by
locked_until
```

or an optimistic `version` plus short claim state.

Rules:

- claim transaction is short
- no DB row lock is held during remote network calls
- expired leases become claimable again
- retry is safe because remote mutations are idempotent/reconcilable
- lease duration exceeds expected single-attempt duration but is bounded

---

## 19. Saga/manual intervention terminal state

Not every failure should retry forever.

Durable workflows need a state indicating automated recovery exhausted its policy and human/operator action is required.

Example:

```text
FAILED_REQUIRES_ATTENTION
```

or equivalent metadata on FAILED.

Persist:

```text
last_error_code
last_error_message
retry_count
last_attempt_at
manual_review_reason
```

This state does not silently discard the workflow.

Operational tooling can later expose a safe replay/reconcile command.

---

## 20. Manual recovery must be an idempotent command

Do not fix production state by directly editing multiple service databases.

Future admin/operator commands may include:

```text
ReconcilePayment(booking_id)
RetryCancellation(cancellation_id)
RequeueOutboxEvent(event_id)
ReconcileRefund(refund_id)
```

They call normal usecases and preserve invariants/auditability.

Direct SQL is reserved for exceptional break-glass procedures with explicit runbook/audit.

---

## 21. Outbox operational policy

Outbox publication failure does not roll back committed domain state.

Publisher rules:

- bounded batch claim
- short claim transaction
- publish outside DB transaction
- mark published after broker acknowledgement
- duplicate publication is acceptable
- retry with backoff
- expose pending count/oldest age/failure count
- after automated retry threshold, retain event and alert rather than delete it

A replay tool can safely republish because consumers are idempotent.

---

## 22. Kafka consumer operational policy

Consumers assume at-least-once delivery.

Rules:

- process business/local DB transaction first
- commit offset only after successful durable handling
- classify poison/permanent failures
- avoid infinite hot-loop on one bad message
- route unrecoverable records to DLQ/quarantine with original metadata
- expose consumer lag and DLQ rate

Consumer restart must not cause duplicate external side effects because local processed-event/job boundaries are idempotent.

---

## 23. Database migrations: expand and contract

Rolling deployments mean old and new application versions can run concurrently.

Use backward-compatible migration phases.

### Expand

Examples:

```text
add nullable column
add new table/index
add new enum representation without removing old behavior
write both old/new forms if required
```

Deploy code that can work with both schemas.

### Migrate/backfill

Run bounded/resumable data migration if needed.

### Contract

Only after all running code no longer depends on old schema:

```text
drop old column
remove old index/constraint
remove compatibility code
```

Avoid one deployment that both renames/drops a column and expects all instances to update atomically.

---

## 24. Migration execution ownership

Production migrations should run as an explicit deployment/migrator step rather than every service replica automatically racing migrations during startup.

Local development may use convenience automation.

Production pattern:

```text
migration job
    ↓ success
application rollout
```

Migration scripts are versioned and reviewable.

Long index builds/backfills need production-safe planning.

---

## 25. Seed/reference data

Do not mix destructive test seed behavior with production migrations.

Keep separate:

```text
schema migrations
local/test fixtures
optional reference-data migrations
```

Local demo setup may seed deterministic hotels, room types, inventory, rates and users through explicit commands/scripts.

---

## 26. Configuration

Each service uses typed configuration validated at startup.

Categories:

```text
server ports
DB DSN/pool limits
Kafka brokers/topics
remote service addresses
operation deadlines
worker concurrency/batch size
retry/backoff policy
JWT/JWKS settings
provider credentials
observability exporters
```

Invalid required configuration fails startup clearly.

Business logic must not call `os.Getenv` directly throughout the codebase.

---

## 27. Secrets

Secrets must not be committed.

Local development:

```text
.env / environment variables
```

Production:

```text
secret manager / orchestrator secret injection
```

Secret values must not appear in logs, traces, metrics labels, Kafka payloads, or error responses.

Payment method/provider tokens are treated as sensitive references, not arbitrary logged strings.

---

## 28. Structured logging

Logs are structured and include stable correlation context where available:

```text
timestamp
level
service
operation
request_id
correlation_id
trace_id
booking_id / reservation_id / payment_id when safe
error_code
```

Do not log:

```text
passwords
JWTs
refresh tokens
provider secrets
full payment method credentials
unnecessary PII
```

Avoid logging every retry as ERROR when expected transient behavior would create alert noise; severity reflects operational meaning.

---

## 29. Metrics: separate business and infrastructure signals

Business examples:

```text
booking_create_total{outcome}
booking_cancel_total{outcome}
reservation_reserve_total{outcome}
payment_total{outcome}
refund_total{outcome}
saga_recovery_total{workflow,outcome}
```

Infrastructure examples:

```text
grpc_client_duration
http_server_duration
db_query_duration
db_pool_wait
kafka_publish_failures
consumer_lag
outbox_pending
```

Never use IDs such as booking_id/user_id as metric labels.

---

## 30. SLO-oriented signals

For portfolio/production design, define initial service-level indicators rather than pretending arbitrary thresholds are guarantees.

Customer-facing examples:

```text
Search success/latency
Quote success/latency
CreateBooking terminal known-outcome rate
Booking query success/latency
CancelBooking accepted/rejected/unknown outcome rate
```

Correctness/recovery examples:

```text
oversell invariant violations = 0
unreconciled payment age
unreconciled refund age
oldest incomplete Saga age
outbox publication lag
```

Some of the most important SLOs are backlog-age/recovery-time rather than only request latency.

---

## 31. Alert philosophy

Alert on symptoms requiring action, not every individual transient error.

Good candidates:

```text
sustained error-rate increase
p95/p99 latency breach
DB pool saturation
DB lock-wait spike
oldest Saga age above threshold
outbox oldest pending age above threshold
Kafka consumer lag continuously growing
DLQ events present/growing
payment/refund UNKNOWN age above threshold
inventory invariant violation
```

A single expected retry usually belongs in logs/metrics, not paging.

---

## 32. Failure injection scenarios

Production-hardening tests should deliberately inject:

```text
Availability gRPC timeout
Payment gRPC lost response after success
provider timeout after charge/refund success
Booking process crash after remote success before local persistence
PostgreSQL temporary disconnect
lock contention / DB pool exhaustion
Kafka unavailable while booking commits
outbox publisher crash after Kafka publish before mark-published
consumer crash after local commit before offset commit
Notification provider outage
SIGTERM during active request and during active worker operation
```

Expected result is defined by existing Saga/outbox/idempotency architecture, not simply “request returns 200”.

---

## 33. Dependency outage behavior matrix

### Booking DB unavailable

Booking cannot safely serve mutations/reads requiring authoritative state.

```text
readiness may fail
requests fail 503/appropriate unavailable response
```

### Kafka unavailable

Booking request path continues while outbox accumulates, subject to operational backlog/storage safeguards.

### Availability unavailable

New bookings cannot reserve inventory. Booking records/recovery behavior depends on the point in Saga and returns/continues accurately.

### Payment unavailable

No guessing. Booking remains recoverable; payment state follows known/unknown semantics.

### Pricing unavailable

New exact quotes/bookings requiring quote validation fail unavailable; existing confirmed booking queries/cancellations do not depend on live Pricing because price/policy are snapshotted.

### Notification unavailable

Booking correctness is unaffected; notification job backlog grows and alerts.

---

## 34. Correctness under overload

When overloaded, the system should reject/defer work rather than weaken invariants.

Never respond to overload by:

```text
skipping database locks
trusting stale cache for reservation mutation
bypassing idempotency constraints
dropping outbox events
blindly retrying financial operations
```

Availability degradation is preferable to corrupt booking/inventory/payment state.

---

## 35. Deployment model

Initial production-oriented deployment assumes stateless service replicas plus service-owned PostgreSQL databases/logical databases and shared infrastructure such as Kafka/observability.

Service replicas may scale horizontally because:

- request state is not kept only in process memory
- worker claims are database/Kafka coordinated
- Saga/outbox operations are durable/idempotent
- JWT validation is stateless at Gateway

Do not rely on sticky sessions for business correctness.

---

## 36. Kubernetes startup/shutdown probes

Conceptual endpoints:

```text
/health/live
/health/ready
```

Optional startup probe protects slow initialization without misusing liveness.

Readiness is removed before graceful drain.

Probe handlers belong to Application/infrastructure composition and must not contain domain behavior.

---

## 37. Rolling deployment compatibility

A rolling deploy may temporarily run version N and N+1 together.

Therefore:

- protobuf/event changes remain backward compatible within supported versions
- DB migrations use expand/contract
- new event fields are additive when possible
- consumers handle supported old/new event versions
- no deployment assumes all replicas switch atomically

Breaking contract changes require a versioned API/event migration plan.

---

## 38. Reliability testing layers

### Unit/domain tests

State transitions, retry classification, policy calculations.

### Repository integration tests

Locking, constraints, transaction atomicity, migration behavior.

### Service integration tests

Idempotent gRPC commands, Saga recovery, provider fakes.

### End-to-end tests

Gateway -> services -> databases/Kafka.

### Fault-injection tests

Kill/restart/timeouts/dependency outage.

### Load tests

Contention, saturation and recovery behavior under concurrency.

A passing happy-path E2E test is not sufficient evidence of distributed-system correctness.

---

## 39. Architecture boundaries for reliability code

Cross-cutting reliability helpers belong in infrastructure/shared technical packages, for example:

```text
pkg/grpcx
pkg/httpx
pkg/retry
pkg/observability
pkg/shutdown
```

But business-specific interpretation remains in usecase/domain.

Example:

```text
gRPC adapter classifies DeadlineExceeded
        ↓
PaymentRepository returns PaymentOutcomeUnknown / classified error
        ↓
Booking usecase persists PAYMENT_UNKNOWN
```

A generic interceptor must not decide Booking state.

---

## 40. Baseline architecture decisions

Unless superseded by an ADR:

1. Correctness and recoverability take priority over guessed availability.
2. Every operation is classified before retry behavior is chosen.
3. Retry ownership is explicit to avoid retry amplification.
4. Context/deadline propagates end-to-end; child calls may only shorten it.
5. Financial timeout/lost-response outcomes are reconciled, not blindly retried.
6. Circuit breakers are infrastructure protection, not business truth.
7. DB pools, worker concurrency and in-memory queues are bounded.
8. Durable queues live in PostgreSQL/Kafka, not unbounded process memory.
9. Liveness does not depend on transient downstream health.
10. Readiness checks only dependencies required to safely accept new synchronous work and avoids deep cascading health graphs.
11. Kafka outage does not make Booking unready because outbox decouples publication.
12. Graceful shutdown marks not-ready, stops new work, drains bounded in-flight operations and preserves recoverability.
13. Durable worker claims use short leases/optimistic state; no row lock is held during network calls.
14. Automated recovery has bounded retry and a visible manual-attention state.
15. Operator recovery is done through idempotent usecases/commands rather than normal cross-service SQL editing.
16. Database migrations follow expand/backfill/contract for rolling-deploy compatibility.
17. Production migrations run as an explicit deployment step/job rather than every replica racing at startup.
18. Config is typed/validated; secrets are injected and never logged.
19. Observability includes backlog age/recovery lag, not only request latency.
20. Overload must never weaken inventory/payment/idempotency invariants.

---

## 41. Implementation consequences

Existing hardening tasks should implement these policies rather than inventing retry/shutdown/probe behavior independently.

Likely implementation scope includes:

- shared deadline/retry classification helpers and adapter policies
- health/live/readiness endpoints
- graceful shutdown for HTTP/gRPC/workers/Kafka consumers
- bounded DB pools/worker concurrency configuration
- outbox/Saga backlog metrics and manual-attention state
- expand/contract migration workflow and deployment migration job
- typed config/startup validation and secret-safe logging
- failure-injection tests covering shutdown/dependency outages/retry amplification

If backlog tasks do not explicitly cover these boundaries, PM/BA should refine existing tasks or insert narrowly scoped implementation tickets before deployment completion.
