# Clean Architecture Conventions

## Goal

All microservices in this repository follow the same dependency direction:

```text
Application
    |
    v
Usecase
    |
    v
Repository interfaces / Domain
    |
    v
Infrastructure implementations
```

The main rule is that outer layers may depend on inner abstractions, but inner layers must not depend on transport, persistence, or vendor-specific code.

## Layer responsibilities

### Application

Application contains entry points only.

Examples:

- gRPC handlers
- HTTP handlers
- Kafka consumers
- scheduled workers
- CLI commands

Application responsibilities:

- parse/validate transport shape
- map transport request to usecase input
- call a usecase
- map usecase output to transport response
- map domain/usecase errors to transport errors
- propagate request context

Application must not:

- contain business rules
- query PostgreSQL directly
- call Redis directly
- call Kafka directly
- call another gRPC service directly
- own transaction boundaries

Example flow:

```text
gRPC request
    |
    v
application/grpc handler
    |
    v
usecase.Execute(...)
```

### Usecase

Usecase owns application business orchestration.

Responsibilities:

- coordinate domain entities
- enforce business workflow
- decide transaction boundaries
- call repository/gateway interfaces
- execute Saga steps and compensations
- return stable usecase outputs/errors

Usecase must not depend on:

- protobuf-generated types
- grpc.ClientConn
- pgx/sqlc/GORM
- Kafka client libraries
- Redis client libraries
- HTTP SDK details

### Domain

Domain contains business concepts that remain useful without infrastructure.

Typical contents:

```text
domain/
├── entity/
├── repository/
├── errors.go
└── service/
```

Entities should contain behavior when the rule naturally belongs to the entity, for example:

```go
func (r *Reservation) Confirm(now time.Time) error
```

rather than being passive database structs.

Repository interfaces belong to the inner side of the architecture. They describe capabilities needed by usecases, not database tables.

### Infrastructure

Infrastructure contains concrete adapters.

Examples:

```text
infrastructure/
├── postgres/
├── redis/
├── kafka/
├── grpcclient/
├── config/
└── observability/
```

Infrastructure may import vendor libraries and generated clients because it sits at the outer boundary.

## Repository abstraction

A repository is any abstraction used by a usecase to communicate with an external dependency.

Examples:

- PostgreSQL repository
- Redis repository
- Kafka event publisher
- gRPC client to another microservice
- external payment provider

For example Booking Service does not depend directly on the generated Availability gRPC client.

Usecase-facing interface:

```go
type AvailabilityRepository interface {
    ReserveInventory(ctx context.Context, req ReserveInventoryRequest) (*Reservation, error)
    ConfirmReservation(ctx context.Context, reservationID string) error
    ReleaseReservation(ctx context.Context, reservationID string, reason ReleaseReason) error
    GetReservation(ctx context.Context, bookingID string) (*Reservation, error)
}
```

Concrete implementation:

```text
infrastructure/grpcclient/availability_repository.go
```

This adapter maps domain/usecase models to protobuf and maps gRPC errors back to stable repository/domain errors.

## Transaction ownership

Transaction boundaries are decided by usecases.

Preferred explicit pattern:

```go
err := uc.repo.WithTx(ctx, func(tx repository.TxRepository) error {
    // one business operation
    return nil
})
```

Repositories must not silently open independent transactions for steps that are required to be atomic together.

## Cross-service communication

From the usecase perspective, another microservice is an external dependency and therefore accessed through an interface.

Example:

```text
CreateBookingUsecase
    |
    +--> PricingRepository
    +--> AvailabilityRepository
    +--> PaymentRepository
    +--> BookingRepository
```

Concrete gRPC calls live in infrastructure adapters.

## Application entry points

Application is not limited to HTTP/gRPC.

The following are also application entry points:

```text
application/grpc/
application/http/
application/consumer/
application/worker/
```

Example reservation expiration flow:

```text
ExpirationWorker
    |
    v
ExpireReservationUsecase
    |
    v
AvailabilityRepository
```

The worker must not query the database directly.

## Shared code rule

Do not create a large shared business library between services.

Safe shared packages are technical primitives such as:

- logger setup
- tracing setup
- gRPC interceptor utilities
- configuration helpers
- generated protobuf packages

Domain logic remains inside each owning service.

## Suggested service layout

```text
services/<service>/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── application/
│   │   ├── grpc/
│   │   ├── http/
│   │   ├── consumer/
│   │   └── worker/
│   ├── usecase/
│   ├── domain/
│   │   ├── entity/
│   │   ├── repository/
│   │   ├── service/
│   │   └── errors.go
│   └── infrastructure/
│       ├── postgres/
│       ├── redis/
│       ├── kafka/
│       ├── grpcclient/
│       ├── config/
│       └── observability/
└── migrations/
```

## Dependency rule summary

Allowed:

```text
application -> usecase
usecase -> domain
usecase -> repository interfaces
infrastructure -> domain/repository interfaces
cmd/main -> all concrete wiring
```

Not allowed:

```text
usecase -> infrastructure
usecase -> protobuf
usecase -> pgx
usecase -> kafka library
usecase -> generated gRPC client
application -> postgres
application -> kafka
```

The composition root in `cmd/server/main.go` is the place where concrete infrastructure implementations are constructed and injected into usecases and application handlers.