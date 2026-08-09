# Hotel Booking Platform

A production-oriented hotel booking platform built with **Go**, designed as a hands-on portfolio project for practicing microservices, gRPC, event-driven architecture, distributed-system reliability, and observability.

The goal of this project is not to build a large CRUD application. Instead, it focuses on the engineering problems that appear in real backend systems: service boundaries, synchronous and asynchronous communication, concurrent booking, retries, idempotency, distributed transactions, failure recovery, and tracing.

## Project Goals

- Build a realistic microservice system in Go.
- Use **gRPC + Protocol Buffers** for synchronous service-to-service communication.
- Use **Kafka** for asynchronous domain events.
- Prevent double booking under concurrent requests.
- Handle payment and booking workflows with Saga-style compensation.
- Guarantee reliable event publishing with the Transactional Outbox pattern.
- Design idempotent APIs and consumers.
- Add production-grade metrics, logs, distributed tracing, and health checks.
- Run the complete platform locally with Docker Compose and later deploy it with Kubernetes.

## High-Level Architecture

```text
                           Web / Mobile Client
                                   |
                               REST / HTTP
                                   |
                                   v
                          +------------------+
                          |   API Gateway    |
                          +--------+---------+
                                   |
                                  gRPC
                  +----------------+----------------+
                  |                                 |
                  v                                 v
          +---------------+                 +----------------+
          | Auth Service  |                 | Booking Service|
          +---------------+                 +-------+--------+
                                                  |
                           +----------------------+----------------------+
                           |                      |                      |
                          gRPC                   gRPC                   gRPC
                           |                      |                      |
                           v                      v                      v
                +------------------+   +------------------+   +----------------+
                | Availability    |   | Pricing Service  |   | Payment Service|
                | Service         |   +------------------+   +-------+--------+
                +--------+--------+                                 |
                         |                                          |
                         +-------------------+----------------------+
                                             |
                                            Kafka
                                             |
                                             v
                                  +----------------------+
                                  | Notification Service |
                                  +----------------------+
```

## Services

### API Gateway

Public entry point for web and mobile clients.

Responsibilities:

- Expose REST APIs.
- Authenticate requests.
- Generate and propagate request IDs and tracing metadata.
- Translate external HTTP requests into internal gRPC calls.
- Apply request-level timeout and rate limiting.

### Auth Service

Handles identity and authorization concerns.

Responsibilities:

- User registration and login.
- Access and refresh tokens.
- Role-based authorization.
- User identity lookup for internal services.

### Booking Service

Owns the booking lifecycle and acts as the central workflow coordinator.

Responsibilities:

- Create and cancel bookings.
- Coordinate availability reservation and payment.
- Maintain booking state.
- Execute compensation when part of the workflow fails.
- Publish booking domain events through an outbox.

Example booking states:

```text
PENDING
  |
  +--> RESERVED
          |
          +--> PAYMENT_PROCESSING
                    |
             +------+------+
             |             |
             v             v
        CONFIRMED       CANCELLED
```

### Availability Service

Owns room inventory and reservation state.

Responsibilities:

- Check whether rooms are available for a date range.
- Temporarily reserve inventory.
- Release expired or cancelled reservations.
- Prevent double booking when multiple requests arrive concurrently.

A key engineering scenario for this service is ensuring that two users cannot successfully reserve the final available room at the same time.

Possible concurrency strategies to evaluate:

- Optimistic locking.
- Atomic SQL updates.
- `SELECT ... FOR UPDATE`.
- Redis-based distributed locking where appropriate.

The final implementation should document why a specific approach was selected and what trade-offs it introduces.

### Pricing Service

Calculates the final price for a reservation.

Responsibilities:

- Base room pricing.
- Seasonal pricing.
- Date-range pricing.
- Discounts and promotions.
- Taxes and fees.

Keeping pricing separate makes it possible to evolve pricing rules independently from booking orchestration.

### Payment Service

Simulates integration with an external payment provider.

Responsibilities:

- Authorize payments.
- Capture payments.
- Refund or void payments during compensation.
- Store provider transaction references.
- Guarantee idempotent payment operations.

Payment APIs must accept an idempotency key so retries cannot accidentally charge the same booking twice.

Example:

```text
booking-98342-payment
```

If a payment succeeds but the gRPC response is lost, a retry using the same key returns the previous result instead of creating another charge.

### Notification Service

Consumes events asynchronously from Kafka.

Responsibilities:

- Booking confirmation notifications.
- Booking cancellation notifications.
- Payment failure notifications.
- Future integration with email, SMS, or push providers.

Notification delivery is intentionally asynchronous because the booking request should not depend on an external notification provider being available.

## Communication Model

The platform intentionally uses different communication styles depending on the requirement.

| Communication | Technology | Example |
| --- | --- | --- |
| Client to backend | REST/HTTP | Create booking |
| Internal synchronous calls | gRPC | Booking -> Availability |
| Domain events | Kafka | BookingConfirmed |
| Cache / temporary state | Redis | Reservation / rate-limit state |
| Persistent storage | PostgreSQL | Booking and payment data |

### gRPC

gRPC is used when the caller needs an immediate response from another service.

Examples:

```text
Booking Service --gRPC--> Availability Service
Booking Service --gRPC--> Pricing Service
Booking Service --gRPC--> Payment Service
```

The implementation should support:

- Protocol Buffers contracts.
- Unary interceptors.
- Request metadata propagation.
- Structured logging.
- Authentication metadata.
- Distributed tracing.
- Deadlines and timeout propagation.
- Error mapping.
- Graceful shutdown.

An important rule is that downstream calls should reuse the upstream context rather than creating a new `context.Background()`, so cancellation and deadlines propagate through the request chain.

### Kafka

Kafka is used for asynchronous domain events where services do not need an immediate response.

Initial events may include:

```text
BookingCreated
RoomReserved
PaymentAuthorized
PaymentFailed
BookingConfirmed
BookingCancelled
ReservationExpired
```

Consumers must be written with the assumption that messages can be delivered more than once.

Therefore handlers should be idempotent.

## Booking Flow

The initial successful flow is:

```text
Client
  |
  | POST /api/v1/bookings
  v
API Gateway
  |
  | gRPC
  v
Booking Service
  |
  +--> Pricing Service
  |
  +--> Availability Service: Reserve()
  |
  +--> Payment Service: Authorize()
  |
  +--> Persist CONFIRMED booking
  |
  +--> Transactional Outbox
  |
  v
Kafka: BookingConfirmed
  |
  v
Notification Service
```

## Failure and Compensation Flow

Distributed systems must be designed around failure rather than assuming every call succeeds.

Example:

```text
Reserve room
    |
    v
Payment authorization
    |
    X failure
    |
    v
Release room reservation
    |
    v
Mark booking CANCELLED
```

The Booking Service will initially implement Saga orchestration so the workflow and compensation logic remain easy to understand.

Future iterations can explore choreography and compare the trade-offs.

## Transactional Outbox

A common failure case is successfully committing a booking to PostgreSQL but failing to publish the corresponding Kafka event.

To avoid inconsistent state, the Booking Service will persist both changes in a single database transaction:

```text
BEGIN

INSERT INTO bookings (...);
INSERT INTO outbox_events (...);

COMMIT
```

A background publisher then reads pending outbox records and publishes them to Kafka.

This provides reliable event publishing without requiring a distributed transaction between PostgreSQL and Kafka.

## Reliability Topics

The project should demonstrate more than successful request flows.

Failure scenarios to implement and document include:

- gRPC service unavailable.
- gRPC deadline exceeded.
- Payment succeeds but response is lost.
- Duplicate client request.
- Duplicate Kafka event.
- Consumer crashes after processing but before acknowledgement.
- Kafka temporarily unavailable.
- Booking Service crashes during a workflow.
- Reservation expires before payment completes.
- Two users attempt to reserve the final room concurrently.

Reliability mechanisms will include:

- Timeouts.
- Controlled retries with exponential backoff.
- Idempotency keys.
- Idempotent consumers.
- Dead-letter queues.
- Transactional Outbox.
- Saga compensation.
- Circuit breaker where useful.
- Graceful shutdown.

## Data Ownership

Each service owns its data and other services should access it through contracts instead of reading another service's tables directly.

Initial storage design:

```text
Auth Service         -> PostgreSQL
Booking Service      -> PostgreSQL
Availability Service -> PostgreSQL
Pricing Service      -> PostgreSQL
Payment Service      -> PostgreSQL
Notification Service -> PostgreSQL (optional delivery history)
```

For local development these databases may initially run on the same PostgreSQL instance using separate databases or schemas. The logical ownership boundary remains per service.

## Observability

The platform will include observability from the early stages instead of adding it only at the end.

### Distributed Tracing

OpenTelemetry will propagate traces across:

```text
HTTP request
    -> API Gateway
        -> gRPC Booking
            -> gRPC Availability
            -> gRPC Pricing
            -> gRPC Payment
```

This makes it possible to inspect latency and failures across a complete booking request.

### Metrics

Prometheus metrics may include:

- HTTP request rate and latency.
- gRPC request rate and latency.
- Booking success/failure rate.
- Payment failure rate.
- Active reservation count.
- Kafka consumer lag.
- Outbox backlog.
- Retry counts.

Grafana will be used for dashboards.

### Logging

Services should produce structured logs containing fields such as:

```text
request_id
trace_id
service
operation
booking_id
user_id
latency_ms
error
```

## Proposed Technology Stack

| Area | Technology |
| --- | --- |
| Language | Go |
| Public API | REST / HTTP |
| Internal RPC | gRPC |
| Contracts | Protocol Buffers |
| Event streaming | Kafka |
| Primary database | PostgreSQL |
| Cache / ephemeral state | Redis |
| Migrations | TBD |
| Observability | OpenTelemetry |
| Metrics | Prometheus |
| Dashboards | Grafana |
| Local infrastructure | Docker Compose |
| Deployment | Kubernetes |
| Load testing | k6 |

Technology choices may change during implementation when there is a documented architectural reason.

## Proposed Repository Structure

```text
hotel-booking/
├── services/
│   ├── gateway/
│   ├── auth/
│   ├── booking/
│   ├── availability/
│   ├── pricing/
│   ├── payment/
│   └── notification/
│
├── proto/
│   ├── booking/v1/
│   ├── availability/v1/
│   ├── pricing/v1/
│   └── payment/v1/
│
├── pkg/
│   ├── config/
│   ├── grpcx/
│   ├── logger/
│   └── tracing/
│
├── deployments/
│   ├── docker-compose.yml
│   └── kubernetes/
│
├── observability/
│   ├── prometheus/
│   ├── grafana/
│   └── otel/
│
├── docs/
│   ├── architecture.md
│   ├── grpc.md
│   ├── saga.md
│   └── failure-scenarios.md
│
├── Makefile
├── buf.yaml
├── buf.gen.yaml
└── README.md
```

## Implementation Roadmap

### Milestone 1 - Foundation

- Initialize Go workspace/modules.
- Define repository structure.
- Add Docker Compose.
- Start PostgreSQL, Redis, and Kafka locally.
- Add shared configuration and structured logging.
- Add health endpoints.
- Configure Protocol Buffers and Buf.
- Implement a minimal gRPC request between two services.

### Milestone 2 - Core Booking Flow

- Implement Booking Service.
- Implement Availability Service.
- Design room and reservation schema.
- Create booking and availability protobuf contracts.
- Implement booking -> availability gRPC communication.
- Implement concurrent reservation protection.
- Add integration tests for double-booking scenarios.

### Milestone 3 - Pricing, Payment, and Saga

- Implement Pricing Service.
- Implement Payment Service.
- Add idempotency keys to payment operations.
- Implement booking workflow orchestration.
- Implement compensation when payment fails.
- Add timeout and retry policies.

### Milestone 4 - Event-Driven Architecture

- Add Kafka infrastructure.
- Implement Transactional Outbox.
- Publish booking domain events.
- Implement Notification Service.
- Add idempotent consumers.
- Add retry and dead-letter handling.

### Milestone 5 - Reliability

- Add gRPC interceptors.
- Propagate deadlines and cancellation.
- Add circuit-breaker experiments.
- Simulate service failures.
- Test duplicate requests and duplicate events.
- Test workflow recovery after crashes.
- Document important failure scenarios and design decisions.

### Milestone 6 - Production Polish

- Add OpenTelemetry tracing.
- Add Prometheus metrics.
- Add Grafana dashboards.
- Add k6 load tests.
- Profile Go services under load.
- Add Kubernetes manifests.
- Add CI checks.
- Document benchmark results and architectural trade-offs.

## Portfolio Focus

This repository is intended to demonstrate backend engineering skills rather than only framework usage.

The final project should make it easy to discuss questions such as:

- Why was gRPC chosen for this call instead of Kafka?
- What happens when a worker or service crashes midway through a booking?
- How is double booking prevented?
- How does the system avoid duplicate payment charges?
- What happens if Kafka is unavailable after the booking transaction commits?
- How are deadlines propagated across services?
- How can a request be traced across HTTP, gRPC, database calls, and Kafka consumers?
- What consistency guarantees does each workflow provide?

Architectural decisions and trade-offs should be documented as the project evolves.
