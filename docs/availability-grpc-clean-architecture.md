# Availability Service: gRPC Contract and Clean Architecture

## Purpose

This document defines how the Availability Service exposes gRPC entry points while preserving a strict Clean Architecture dependency direction.

The service follows this rule:

```text
Application
    ↓
Usecase
    ↓
Repository interfaces
    ↓
Infrastructure implementations
```

`application` contains entry points only. It must not contain business rules, database queries, or transaction logic.

## Dependency direction

```text
gRPC Handler
    ↓
Usecase
    ↓
Domain / Repository Interfaces
    ↓
Infrastructure
    ↓
PostgreSQL / Kafka / External Systems
```

The usecase layer must not depend on protobuf, gRPC, PostgreSQL drivers, Kafka clients, Redis clients, or other transport/infrastructure libraries.

## Proposed service structure

```text
services/availability/
├── cmd/
│   └── server/
│       └── main.go
│
├── internal/
│   ├── application/
│   │   ├── grpc/
│   │   │   ├── handler.go
│   │   │   ├── mapper.go
│   │   │   └── error_mapper.go
│   │   └── worker/
│   │       └── reservation_expiration_worker.go
│   │
│   ├── usecase/
│   │   ├── check_availability.go
│   │   ├── reserve_inventory.go
│   │   ├── confirm_reservation.go
│   │   ├── release_reservation.go
│   │   └── expire_reservation.go
│   │
│   ├── domain/
│   │   ├── entity/
│   │   │   ├── inventory.go
│   │   │   └── reservation.go
│   │   ├── repository/
│   │   │   ├── availability_repository.go
│   │   │   └── transaction.go
│   │   └── errors.go
│   │
│   └── infrastructure/
│       ├── postgres/
│       ├── kafka/
│       ├── config/
│       └── observability/
│
├── migrations/
└── proto/
```

## Application layer responsibilities

The application layer is an adapter/entry-point layer only.

For gRPC it should:

1. receive protobuf requests;
2. map protobuf messages into usecase inputs;
3. call the corresponding usecase;
4. map usecase output back to protobuf;
5. map domain/usecase errors to gRPC status codes.

It must not:

- query PostgreSQL;
- open business transactions;
- implement reservation state transitions;
- calculate availability;
- publish domain events directly as part of business logic.

Example flow:

```text
protobuf request
      ↓
application/grpc handler
      ↓
usecase input
      ↓
usecase.Execute
      ↓
usecase output
      ↓
protobuf response
```

## Application worker entry points

Application is not limited to gRPC handlers. Background workers are also entry points.

For example:

```text
application/worker
      ↓
ExpireReservationUsecase
      ↓
Repository
```

The expiration worker may own scheduling/ticker concerns, but it must not query the database directly.

## Proto layout

```text
proto/
└── availability/
    └── v1/
        └── availability.proto
```

Recommended package:

```proto
syntax = "proto3";

package availability.v1;

option go_package =
    "github.com/liemdang260/hotel-booking/gen/availability/v1;availabilityv1";
```

## Availability gRPC service

```proto
service AvailabilityService {
  rpc CheckAvailability(CheckAvailabilityRequest)
      returns (CheckAvailabilityResponse);

  rpc ReserveInventory(ReserveInventoryRequest)
      returns (ReserveInventoryResponse);

  rpc GetReservation(GetReservationRequest)
      returns (GetReservationResponse);

  rpc ConfirmReservation(ConfirmReservationRequest)
      returns (ConfirmReservationResponse);

  rpc ReleaseReservation(ReleaseReservationRequest)
      returns (ReleaseReservationResponse);
}
```

## CheckAvailability

Request concept:

```proto
message CheckAvailabilityRequest {
  string hotel_id = 1;
  string room_type_id = 2;
  google.type.Date check_in = 3;
  google.type.Date check_out = 4;
  int32 quantity = 5;
}
```

Response concept:

```proto
message CheckAvailabilityResponse {
  bool available = 1;
  int32 available_quantity = 2;
}
```

Usecase types are independent from protobuf:

```go
type CheckAvailabilityInput struct {
    HotelID    string
    RoomTypeID string
    CheckIn    time.Time
    CheckOut   time.Time
    Quantity   int
}

type CheckAvailabilityOutput struct {
    Available         bool
    AvailableQuantity int
}
```

## ReserveInventory

Request concept:

```proto
message ReserveInventoryRequest {
  string booking_id = 1;
  string hotel_id = 2;
  string room_type_id = 3;
  google.type.Date check_in = 4;
  google.type.Date check_out = 5;
  int32 quantity = 6;
}
```

Response concept:

```proto
message ReserveInventoryResponse {
  Reservation reservation = 1;
}
```

The usecase must accept its own input type rather than protobuf messages:

```go
type ReserveInventoryInput struct {
    BookingID  string
    HotelID    string
    RoomTypeID string
    CheckIn    time.Time
    CheckOut   time.Time
    Quantity   int
}
```

## Reservation message

```proto
message Reservation {
  string reservation_id = 1;
  string booking_id = 2;
  string hotel_id = 3;
  string room_type_id = 4;
  google.type.Date check_in = 5;
  google.type.Date check_out = 6;
  int32 quantity = 7;
  ReservationStatus status = 8;
  google.protobuf.Timestamp expires_at = 9;
}
```

```proto
enum ReservationStatus {
  RESERVATION_STATUS_UNSPECIFIED = 0;
  RESERVATION_STATUS_HELD = 1;
  RESERVATION_STATUS_BOOKED = 2;
  RESERVATION_STATUS_RELEASED = 3;
  RESERVATION_STATUS_EXPIRED = 4;
}
```

## ConfirmReservation

```proto
message ConfirmReservationRequest {
  string reservation_id = 1;
}

message ConfirmReservationResponse {
  Reservation reservation = 1;
}
```

The application handler only maps and delegates. State transition rules belong in the entity/usecase layer.

## ReleaseReservation

```proto
message ReleaseReservationRequest {
  string reservation_id = 1;
  ReleaseReason reason = 2;
}

message ReleaseReservationResponse {
  Reservation reservation = 1;
}
```

```proto
enum ReleaseReason {
  RELEASE_REASON_UNSPECIFIED = 0;
  RELEASE_REASON_PAYMENT_FAILED = 1;
  RELEASE_REASON_BOOKING_CANCELLED = 2;
  RELEASE_REASON_SAGA_COMPENSATION = 3;
}
```

Release reason is primarily useful for auditability, metrics, and diagnostics. It must not weaken the idempotency guarantees of release behavior.

## GetReservation

```proto
message GetReservationRequest {
  oneof identifier {
    string reservation_id = 1;
    string booking_id = 2;
  }
}

message GetReservationResponse {
  Reservation reservation = 1;
}
```

This RPC supports Saga recovery and reconciliation after Booking Service restarts or loses a previous gRPC response.

## Error model

Do not encode business errors as fields like:

```proto
bool success = 1;
string error = 2;
```

Use gRPC status codes plus structured error details.

Suggested mappings:

| Domain error | gRPC status |
|---|---|
| Invalid date range | InvalidArgument |
| Invalid quantity | InvalidArgument |
| Reservation not found | NotFound |
| Sold out | ResourceExhausted |
| Inventory not configured | FailedPrecondition |
| Reservation expired | FailedPrecondition |
| Idempotency conflict | AlreadyExists |
| Invariant violation | Internal |

Candidate structured detail:

```proto
message AvailabilityErrorDetail {
  AvailabilityErrorCode code = 1;
  string message = 2;
}
```

```proto
enum AvailabilityErrorCode {
  AVAILABILITY_ERROR_CODE_UNSPECIFIED = 0;
  SOLD_OUT = 1;
  INVENTORY_NOT_CONFIGURED = 2;
  RESERVATION_EXPIRED = 3;
  RESERVATION_ALREADY_RELEASED = 4;
  RESERVATION_ALREADY_BOOKED = 5;
  IDEMPOTENCY_CONFLICT = 6;
}
```

Booking Service should inspect structured codes rather than parsing error strings.

## Deadline propagation

The caller owns the request deadline.

Initial target budgets:

```text
CheckAvailability   500ms–1s
ReserveInventory    1–2s
GetReservation      ~500ms
ConfirmReservation  ~1s
ReleaseReservation  ~1s
```

Availability must propagate the incoming context from handler to usecase to repository. Request-bound operations must not replace it with `context.Background()`.

## Retry semantics

| RPC | Retry safety |
|---|---|
| CheckAvailability | Safe |
| GetReservation | Safe |
| ReserveInventory | Safe because booking-based idempotency is enforced |
| ConfirmReservation | Safe because confirmation is idempotent |
| ReleaseReservation | Safe because desired-state release is idempotent |

Retry safety is a business property of the command implementation, not a property provided automatically by gRPC.

## gRPC interceptors

Transport-level cross-cutting concerns belong around the application handler:

```text
gRPC Server
   │
   ├── RecoveryInterceptor
   ├── LoggingInterceptor
   ├── TracingInterceptor
   ├── MetricsInterceptor
   └── AuthInterceptor
          │
          ▼
       Handler
```

Interceptors must not contain domain business rules.

## Domain entities should contain behavior

Avoid data-only entities where every state rule lives in procedural service code.

Example:

```go
type Reservation struct {
    ID        string
    BookingID string
    Status    Status
    ExpiresAt time.Time
}

func (r Reservation) IsExpired(now time.Time) bool {
    return !now.Before(r.ExpiresAt)
}

func (r *Reservation) Confirm(now time.Time) error {
    if r.Status == StatusBooked {
        return nil
    }

    if r.Status != StatusHeld {
        return ErrInvalidReservationState
    }

    if r.IsExpired(now) {
        return ErrReservationExpired
    }

    r.Status = StatusBooked
    return nil
}
```

The usecase coordinates entities and repositories; entities enforce local state rules where appropriate.

## Repository abstractions

Usecases depend only on repository interfaces.

Example:

```go
type AvailabilityRepository interface {
    FindReservationByBookingID(
        ctx context.Context,
        bookingID string,
    ) (*entity.Reservation, error)

    WithTx(
        ctx context.Context,
        fn func(TxRepository) error,
    ) error
}
```

Transaction repository concept:

```go
type TxRepository interface {
    LockInventory(...)
    LockReservation(...)
    CreateReservation(...)
    IncreaseInventory(...)
    DecreaseInventory(...)
    CreateReservationInventory(...)
    UpdateReservationStatus(...)
    InsertOutboxEvent(...)
}
```

## Transaction ownership

Transaction boundaries are decided by the usecase layer.

Preferred pattern:

```go
err := uc.repository.WithTx(
    ctx,
    func(tx repository.TxRepository) error {
        // complete business operation
        return nil
    },
)
```

Do not let each repository method independently open and commit its own business transaction.

## External services are also repository abstractions

The same Clean Architecture rule applies when one service calls another microservice.

For example, Booking Service should depend on an abstraction such as:

```go
type AvailabilityRepository interface {
    ReserveInventory(...)
    ConfirmReservation(...)
    ReleaseReservation(...)
}
```

Its infrastructure implementation may use a generated gRPC client, but the Booking usecase itself must not import `availabilityv1` or gRPC packages.

This same rule will apply to Pricing and Payment integrations.

## Decisions established by this document

- `application` contains entry points/adapters only.
- Usecases own business orchestration and transaction boundaries.
- Domain entities may own local state-transition rules.
- Usecases depend on repository interfaces, never infrastructure implementations.
- PostgreSQL, Kafka, Redis, and gRPC clients belong to infrastructure.
- Generated protobuf types do not cross into the usecase/domain layers.
- Background workers are application entry points and call usecases.
- gRPC errors use status codes plus structured business details.
- Mutating RPCs are designed to be idempotent before retries are enabled.
- Incoming contexts and deadlines propagate through the full request path.

## Next step

Design Booking Service using the same Clean Architecture structure, with `CreateBookingUsecase` as the Saga orchestrator and Pricing, Availability, and Payment represented as repository/gateway interfaces.