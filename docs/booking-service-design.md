# Booking Service Design

## Purpose

Booking Service owns the booking aggregate and orchestrates the main booking Saga. It does not directly know PostgreSQL, protobuf, Kafka, or concrete gRPC clients. All external dependencies are accessed through repository/gateway interfaces.

## Responsibilities

Booking Service owns:

- booking lifecycle
- booking price snapshot
- booking idempotency
- Saga state and recovery
- coordination with Pricing, Availability, and Payment
- Booking domain events through a transactional outbox

Booking Service does not own:

- room inventory
- room pricing rules
- payment provider state
- hotel metadata

## Clean Architecture layout

```text
services/booking/
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
│   │   ├── create_booking.go
│   │   ├── get_booking.go
│   │   ├── cancel_booking.go
│   │   └── recover_booking_saga.go
│   ├── domain/
│   │   ├── entity/
│   │   │   ├── booking.go
│   │   │   ├── price_snapshot.go
│   │   │   └── booking_saga.go
│   │   ├── repository/
│   │   │   ├── booking_repository.go
│   │   │   ├── pricing_repository.go
│   │   │   ├── availability_repository.go
│   │   │   ├── payment_repository.go
│   │   │   └── outbox_repository.go
│   │   └── errors.go
│   └── infrastructure/
│       ├── postgres/
│       ├── grpcclient/
│       ├── kafka/
│       ├── config/
│       └── observability/
└── migrations/
```

## Dependency flow

```text
HTTP/gRPC Handler
      |
      v
CreateBookingUsecase
      |
      +--> BookingRepository
      +--> PricingRepository
      +--> AvailabilityRepository
      +--> PaymentRepository
      |
      v
Domain entities
```

Concrete implementations live outside the usecase:

```text
PricingRepository      -> gRPC Pricing client
AvailabilityRepository -> gRPC Availability client
PaymentRepository      -> gRPC Payment client
BookingRepository      -> PostgreSQL
```

## CreateBooking input

The usecase must not receive protobuf or HTTP request objects.

Conceptual model:

```go
type CreateBookingInput struct {
    IdempotencyKey string
    UserID         string
    HotelID        string
    RoomTypeID     string
    CheckIn        time.Time
    CheckOut       time.Time
    Guests         int
    Rooms          int
    PaymentMethodID string
}
```

## CreateBooking orchestration

Initial Saga flow:

```text
CreateBookingUsecase
       |
       +-- validate input
       |
       +-- resolve idempotency key
       |
       +-- PricingRepository.Quote
       |
       +-- create Booking(PENDING) + Saga(STARTED)
       |
       +-- AvailabilityRepository.ReserveInventory
       |
       +-- persist reservation id + INVENTORY_RESERVED
       |
       +-- PaymentRepository.CreatePayment
       |
       +-- persist payment outcome
       |
       +-- AvailabilityRepository.ConfirmReservation
       |
       +-- local transaction:
       |      Booking -> CONFIRMED
       |      Saga -> COMPLETED
       |      Outbox -> BookingConfirmed
       |
       +-- return booking
```

## Why local state is persisted between remote calls

The Saga must remain recoverable if Booking Service crashes after a remote side effect.

Bad approach:

```text
Reserve
Pay
Confirm
then persist everything
```

A crash between those steps would leave no durable record of what happened.

Preferred approach:

```text
remote side effect
    |
    v
persist resulting Saga state
    |
    v
next remote side effect
```

For example:

```text
ReserveInventory succeeds
    |
    v
persist reservation_id + INVENTORY_RESERVED
    |
    v
CreatePayment
```

## Booking entity

Conceptual behavior:

```go
type Booking struct {
    ID            string
    UserID        string
    HotelID       string
    RoomTypeID    string
    CheckIn       time.Time
    CheckOut      time.Time
    Guests        int
    Rooms         int
    Status        BookingStatus
    ReservationID string
    PaymentID     string
    Price         PriceSnapshot
}
```

The entity should guard valid state transitions rather than letting usecases assign arbitrary status strings.

Example methods:

```go
func (b *Booking) MarkInventoryReserved(reservationID string) error
func (b *Booking) StartPayment() error
func (b *Booking) MarkPaymentUnknown(paymentID string) error
func (b *Booking) Confirm(paymentID string) error
func (b *Booking) FailPayment() error
func (b *Booking) Cancel() error
```

## Booking status

Initial state model:

```text
PENDING
   |
   v
INVENTORY_RESERVED
   |
   v
PAYMENT_PROCESSING
   |\
   | +--> PAYMENT_FAILED
   | +--> PAYMENT_UNKNOWN
   |
   v
CONFIRMED
   |
   v
CANCELLED
```

`PAYMENT_UNKNOWN` is intentionally distinct from `PAYMENT_FAILED` because a timeout does not prove that the provider did not charge the customer.

## Saga entity/state

The booking aggregate and Saga execution state serve related but different purposes.

Suggested Saga state:

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

Suggested Saga fields:

```text
id
booking_id
state
reservation_id
payment_id
last_error
retry_count
next_retry_at
created_at
updated_at
```

## Repository interfaces

### BookingRepository

```go
type BookingRepository interface {
    FindByID(ctx context.Context, id string) (*entity.Booking, error)
    FindByIdempotencyKey(ctx context.Context, key string) (*entity.Booking, error)

    WithTx(
        ctx context.Context,
        fn func(TxBookingRepository) error,
    ) error
}
```

The transaction repository provides only operations that should participate in the local Booking DB transaction.

```go
type TxBookingRepository interface {
    CreateBooking(ctx context.Context, booking *entity.Booking) error
    SaveBooking(ctx context.Context, booking *entity.Booking) error
    CreateSaga(ctx context.Context, saga *entity.BookingSaga) error
    SaveSaga(ctx context.Context, saga *entity.BookingSaga) error
    SaveIdempotencyResult(ctx context.Context, result IdempotencyResult) error
    InsertOutboxEvent(ctx context.Context, event OutboxEvent) error
}
```

### PricingRepository

```go
type PricingRepository interface {
    Quote(ctx context.Context, req QuoteRequest) (*PriceSnapshot, error)
}
```

Infrastructure implementation maps this interface to Pricing Service gRPC.

### AvailabilityRepository

```go
type AvailabilityRepository interface {
    ReserveInventory(ctx context.Context, req ReserveInventoryRequest) (*Reservation, error)
    ConfirmReservation(ctx context.Context, reservationID string) error
    ReleaseReservation(ctx context.Context, reservationID string, reason ReleaseReason) error
    GetReservationByBookingID(ctx context.Context, bookingID string) (*Reservation, error)
}
```

### PaymentRepository

```go
type PaymentRepository interface {
    CreatePayment(ctx context.Context, req CreatePaymentRequest) (*Payment, error)
    GetPayment(ctx context.Context, paymentID string) (*Payment, error)
    RefundPayment(ctx context.Context, paymentID string, amount int64) (*Refund, error)
}
```

## Idempotency

The external `CreateBooking` operation requires an idempotency key.

Application passes the key to the usecase; the usecase owns its semantics.

Rules:

1. New key -> start a new booking.
2. Existing key with identical request -> return/recover the existing booking workflow.
3. Existing key with a different request hash -> reject as idempotency conflict.

The unique constraint in Booking DB is the final concurrency defense.

## Pricing step

Pricing is obtained before inventory/payment and persisted as a snapshot.

```text
PricingRepository.Quote
       |
       v
PriceSnapshot
       |
       v
Booking DB
```

Payment receives the snapshotted amount. It never independently recomputes the booking price.

## Inventory reservation step

Booking Usecase calls the AvailabilityRepository abstraction:

```text
CreateBookingUsecase
      |
      v
AvailabilityRepository
      |
      v
infrastructure/grpcclient
      |
      v
Availability Service
```

If inventory is sold out, the booking can fail before payment starts.

After reservation succeeds, Booking Service persists `reservation_id` and its Saga state before moving to payment.

## Payment step

Payment idempotency is owned by Payment Service, but Booking Service supplies a stable logical key based on the booking/payment attempt.

Possible key:

```text
payment:{booking_id}
```

Payment outcomes:

```text
SUCCEEDED -> continue to ConfirmReservation
FAILED    -> compensate reservation
UNKNOWN   -> do not release inventory immediately; reconcile payment status
```

## Confirm reservation failure after successful payment

Important Saga case:

```text
Payment SUCCEEDED
      |
      v
ConfirmReservation
      |
      X RESERVATION_EXPIRED
```

Booking Service now owns compensation:

```text
RefundPayment
    |
    v
persist compensation result
    |
    v
booking FAILED/CANCELLED
```

A successful payment must never simply be forgotten because a later reservation step failed.

## Payment unknown recovery

On gRPC timeout or ambiguous provider outcome:

```text
Booking -> PAYMENT_UNKNOWN
Saga    -> PAYMENT_UNKNOWN
```

A recovery usecase later calls:

```text
PaymentRepository.GetPayment
```

Outcomes:

```text
SUCCEEDED -> resume ConfirmReservation
FAILED    -> ReleaseReservation
still unknown -> retry later
```

## Saga recovery entry point

Recovery is triggered by an application worker, not by infrastructure directly.

```text
application/worker/SagaRecoveryWorker
             |
             v
RecoverBookingSagaUsecase
             |
             +--> BookingRepository
             +--> AvailabilityRepository
             +--> PaymentRepository
```

The worker only schedules/triggers work. Recovery decisions belong to the usecase.

## Local transaction examples

### Initial booking creation

One local Booking DB transaction:

```text
BEGIN
  insert booking(PENDING)
  insert price snapshot
  insert saga(PRICE_QUOTED)
  insert idempotency record
COMMIT
```

### Reservation persistence

After Availability succeeds:

```text
BEGIN
  booking -> INVENTORY_RESERVED
  set reservation_id
  saga -> INVENTORY_RESERVED
COMMIT
```

### Confirmation

After Payment and reservation confirmation succeed:

```text
BEGIN
  booking -> CONFIRMED
  set payment_id
  saga -> COMPLETED
  insert BookingConfirmed outbox event
  persist idempotency final response
COMMIT
```

No transaction spans another microservice call.

## Outbox

Booking domain events are inserted in the same local transaction as booking state changes.

Initial events:

- `BookingConfirmed`
- `BookingCancelled`
- `BookingFailed`

The outbox publisher is an application worker/usecase combination and does not change the core booking workflow transaction model.

## Application handler

The external handler remains intentionally thin:

```go
func (h *Handler) CreateBooking(ctx context.Context, req *pb.CreateBookingRequest) (*pb.CreateBookingResponse, error) {
    input, err := h.mapper.ToCreateBookingInput(req)
    if err != nil {
        return nil, h.errorMapper.Map(err)
    }

    output, err := h.createBooking.Execute(ctx, input)
    if err != nil {
        return nil, h.errorMapper.Map(err)
    }

    return h.mapper.ToCreateBookingResponse(output), nil
}
```

No Saga logic belongs in this handler.

## Infrastructure gRPC adapter example

Conceptually:

```go
type AvailabilityGRPCRepository struct {
    client availabilityv1.AvailabilityServiceClient
}

func (r *AvailabilityGRPCRepository) ReserveInventory(ctx context.Context, req repository.ReserveInventoryRequest) (*repository.Reservation, error) {
    // map request to protobuf
    // execute gRPC call
    // map protobuf to repository/domain model
    // translate gRPC status into stable errors
}
```

The `CreateBookingUsecase` never imports `availabilityv1`.

## Context and deadlines

Usecases receive the incoming `context.Context` and pass it to repository interfaces.

Infrastructure gRPC adapters may derive shorter child deadlines for specific downstream calls while respecting the parent deadline.

No request path replaces the incoming context with `context.Background()`.

## Retry policy

Retries happen at repository/infrastructure policy boundaries only for operations whose business semantics are idempotent.

Examples:

- `ReserveInventory` is retry-safe because booking ID is idempotent in Availability.
- `CreatePayment` is retry-safe only because Payment enforces its own idempotency key.
- `ConfirmReservation` and `ReleaseReservation` are desired-state/idempotent commands.

Usecases still own decisions about whether a failed step should be retried, reconciled, or compensated.

## Key design rules

1. Application contains entry points only.
2. CreateBooking business orchestration lives in `CreateBookingUsecase`.
3. Usecase calls repository interfaces only.
4. Another microservice is represented to the usecase as a repository/gateway interface.
5. Concrete gRPC clients live in infrastructure.
6. Local DB transaction boundaries are controlled by usecases.
7. No distributed DB transaction spans services.
8. Remote side effects are followed by durable Saga state before continuing.
9. Payment uncertainty must be recoverable.
10. Booking confirmation and its domain event are committed atomically through the outbox.

## Next design step

Finalize the Booking Service persistence schema around these usecase boundaries, then define Booking's external REST/gRPC contract and infrastructure adapter contracts.