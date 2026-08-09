# Hotel Booking Platform — Use Cases

## Purpose

This document defines the first set of business use cases for the Hotel Booking Platform before implementation begins. The goal is to make the service boundaries, APIs, gRPC contracts, events, and failure-handling rules derive from concrete business flows rather than from arbitrary technical separation.

The initial MVP focuses on the booking lifecycle:

`Search -> Check availability -> Hold inventory -> Pay -> Confirm -> Cancel / Expire`

---

## Actors

### Guest
A customer who searches for rooms, creates a booking, pays, views booking details, or cancels a booking.

### Hotel Admin
An operator who manages hotel data, room types, room inventory, pricing, and inventory blocks.

### System
Background processes responsible for reservation expiration, asynchronous event delivery, notifications, retries, and compensating actions.

---

# MVP Use Cases

## UC-01 — Search Hotels and Rooms

### Goal
Allow a guest to search for rooms that match a destination and stay period.

### Input
- Destination or location
- Check-in date
- Check-out date
- Number of guests
- Optional filters such as room type or price range

### Main Flow
1. Guest submits search criteria.
2. The system validates the requested stay period.
3. Catalog data is queried for matching hotels and room types.
4. Availability is checked for the requested dates.
5. Pricing is calculated for the stay.
6. Matching rooms are returned with an estimated total price.

### Notes
Search results are not a reservation guarantee. Availability may change before the guest starts the booking flow.

---

## UC-02 — Check Room Availability

### Goal
Determine whether a requested room type still has inventory for a given date range.

### Main Flow
1. Booking flow requests availability for a room type and stay period.
2. Availability Service calculates remaining inventory for every night in the requested range.
3. If inventory is available for the entire stay, the request succeeds.
4. Otherwise the system returns `NOT_AVAILABLE`.

### Business Rule
A booking can proceed only when inventory exists for every night in the requested stay.

### Internal Communication
Expected to be a synchronous gRPC call between Booking Service and Availability Service.

---

## UC-03 — Create Temporary Reservation

### Goal
Temporarily hold room inventory while the guest completes payment.

### Preconditions
- Room is currently available.
- Booking request is valid.

### Main Flow
1. Guest starts a booking.
2. Booking Service creates a booking in a pending state.
3. Booking Service requests an inventory hold from Availability Service.
4. Availability Service atomically verifies and reserves the requested inventory.
5. A reservation is created with an expiration time.
6. Booking can proceed to payment.

### Reservation Lifecycle

```text
AVAILABLE
   |
   v
HELD
   |
   +---- payment succeeds ----> BOOKED
   |
   +---- payment fails --------> AVAILABLE
   |
   +---- hold expires ---------> AVAILABLE
```

### Concurrency Requirement
If only one room remains and two users attempt to reserve it concurrently, only one hold may succeed.

Potential implementation strategies to evaluate later:
- database row locking
- atomic conditional update
- optimistic locking
- distributed locking where justified

The final design should prefer correctness with minimal unnecessary distributed coordination.

---

## UC-04 — Process Payment

### Goal
Authorize or capture payment for a pending booking.

### Preconditions
- Booking exists.
- Inventory is currently held.
- Hold has not expired.

### Main Flow
1. Booking Service sends a payment request to Payment Service.
2. Payment Service validates the request.
3. Payment Service processes the payment using a payment provider abstraction.
4. On success, Payment Service records the successful transaction.
5. Result is returned to Booking Service.

### Idempotency Requirement
Payment must be idempotent.

A retry caused by timeout or connection failure must not charge the customer twice.

Example idempotency key:

```text
booking:{booking_id}:payment
```

### Failure Example

```text
Booking Service -> Payment Service -> Provider
                                |
                                +-- payment succeeds
                                |
                                X response lost

Booking Service retries request

Payment Service must return the existing successful result instead of charging again.
```

---

## UC-05 — Confirm Booking

### Goal
Finalize a booking after payment succeeds.

### Main Flow
1. Payment succeeds.
2. Booking Service changes booking state to `CONFIRMED`.
3. Held inventory becomes committed inventory.
4. A `BookingConfirmed` domain event is persisted/published.
5. Notification Service asynchronously sends a confirmation message.

### Consistency Requirement
The system must handle failure between database commit and event publication.

A Transactional Outbox pattern is planned to guarantee eventual event delivery.

---

## UC-06 — Handle Payment Failure

### Goal
Release temporarily reserved resources if payment cannot be completed.

### Main Flow
1. Payment fails permanently or exceeds the allowed retry policy.
2. Booking status becomes `PAYMENT_FAILED`.
3. Booking flow requests release of the inventory hold.
4. Reservation becomes `RELEASED`.
5. Room inventory becomes available again.

### Saga Compensation
This flow represents a compensating action for the booking transaction.

```text
Reserve inventory
      |
      v
Process payment
      |
      X
      |
      v
Release inventory
```

---

## UC-07 — Expire Reservation

### Goal
Automatically release inventory when the guest does not complete payment within the reservation window.

### Example Policy
A temporary hold may expire after 10 minutes.

### Main Flow
1. Reservation reaches `expires_at` while still `HELD`.
2. System marks the reservation as `EXPIRED`.
3. Held inventory is released.
4. Booking becomes `EXPIRED` if it has not already progressed.
5. A `ReservationExpired` event is emitted.

### Race Condition to Handle
Payment and reservation expiration may happen near the same time.

The implementation must define one authoritative state transition and prevent both sides from independently succeeding.

---

## UC-08 — View Booking

### Goal
Allow a guest to retrieve current and historical booking information.

### Typical Data
- Booking ID
- Hotel
- Room type
- Check-in / check-out
- Number of guests
- Total amount
- Payment status
- Booking status
- Cancellation information

### Booking Statuses
Initial status set:

```text
PENDING
PENDING_PAYMENT
CONFIRMED
PAYMENT_FAILED
CANCELLED
EXPIRED
```

The exact state machine will be refined during domain design.

---

## UC-09 — Cancel Booking

### Goal
Allow an eligible confirmed booking to be cancelled.

### Preconditions
- Booking exists.
- Booking is in a cancellable state.
- Cancellation policy allows cancellation.

### Main Flow
1. Guest submits cancellation request.
2. Booking Service validates the cancellation policy.
3. Booking status is transitioned to a cancellation state.
4. Refund is initiated if required.
5. Inventory is returned when appropriate.
6. A `BookingCancelled` event is emitted.
7. Notification Service sends cancellation confirmation.

### Important Rule
Cancellation and refund should not be modeled as a single database transaction across services. They should use explicit distributed workflow state and idempotent operations.

---

## UC-10 — Manage Hotel Inventory

### Goal
Allow a hotel administrator to manage sellable inventory.

### Capabilities
- Create and update hotel information
- Create room types
- Configure base inventory
- Block rooms for maintenance
- Update inventory availability
- Configure base pricing

### Scope
This use case is lower priority than the core guest booking flow and can be implemented after the initial booking lifecycle is stable.

---

# Core Domain Models

## Booking

```text
Booking
- id
- user_id
- hotel_id
- room_type_id
- check_in
- check_out
- guest_count
- total_price
- currency
- status
- created_at
- updated_at
```

## Reservation

```text
Reservation
- id
- booking_id
- room_type_id
- quantity
- check_in
- check_out
- expires_at
- status
- created_at
```

## Payment

```text
Payment
- id
- booking_id
- amount
- currency
- idempotency_key
- provider_reference
- status
- created_at
- updated_at
```

---

# Proposed Booking State Flow

```text
                  +------------------+
                  |     PENDING      |
                  +--------+---------+
                           |
                    inventory held
                           |
                           v
                +---------------------+
                |   PENDING_PAYMENT   |
                +----------+----------+
                           |
              +------------+-------------+
              |                          |
        payment success             payment failure
              |                          |
              v                          v
       +-------------+           +----------------+
       |  CONFIRMED  |           | PAYMENT_FAILED |
       +------+------+           +----------------+
              |
          cancellation
              |
              v
       +-------------+
       |  CANCELLED  |
       +-------------+

A pending booking may also transition to EXPIRED when its inventory hold expires.
```

---

# Service Responsibilities Derived From Use Cases

## API Gateway
- Public HTTP/REST API
- Authentication metadata forwarding
- Request correlation
- External API concerns

## Booking Service
- Owns booking lifecycle
- Coordinates the booking workflow
- Enforces booking state transitions
- Initiates compensation when downstream operations fail

## Availability Service
- Owns room inventory
- Checks availability
- Creates inventory holds
- Confirms or releases reservations
- Prevents overselling

## Pricing Service
- Calculates prices for searches and bookings
- Later supports discounts, seasonal pricing, and pricing rules

## Payment Service
- Owns payment transaction state
- Provides idempotent payment operations
- Isolates payment-provider integration
- Supports refund operations

## Notification Service
- Consumes domain events asynchronously
- Sends booking, cancellation, payment, and expiration notifications

---

# Communication Model

## Synchronous
Use gRPC for interactions that require an immediate response.

Examples:

```text
Booking -> Availability: Reserve()
Booking -> Availability: Release()
Booking -> Payment: Pay()
Booking -> Pricing: CalculatePrice()
```

## Asynchronous
Use Kafka for domain events and side effects that do not need to complete inside the caller's request path.

Initial candidate events:

```text
BookingCreated
ReservationCreated
ReservationExpired
PaymentSucceeded
PaymentFailed
BookingConfirmed
BookingCancelled
RefundSucceeded
RefundFailed
```

---

# Failure Scenarios We Intend to Demonstrate

The project should explicitly test and document these scenarios because they provide most of the distributed-systems value of the project.

1. Two guests attempt to book the last available room concurrently.
2. A payment succeeds but the gRPC response times out.
3. Booking Service crashes after payment succeeds but before booking confirmation completes.
4. Database transaction commits but Kafka publication fails.
5. Kafka delivers the same event more than once.
6. Reservation expires while payment is in progress.
7. Client sends the same booking request multiple times.
8. A downstream gRPC service is temporarily unavailable.
9. Notification delivery fails after a booking has already been confirmed.
10. A cancellation request is repeated after the refund has already started.

---

# MVP Boundary

The first implementation should prioritize correctness of the booking lifecycle over feature breadth.

Included initially:
- basic hotel/room search
- availability checks
- temporary inventory hold
- booking creation
- payment simulation/provider abstraction
- booking confirmation
- expiration
- cancellation
- asynchronous notification events

Deferred:
- recommendations
- loyalty points
- coupons and promotions
- advanced dynamic pricing
- reviews
- multi-currency settlement
- complex hotel-admin workflows

---

# Next Design Step

The next technical design task should define the `CreateBooking` sequence in detail, including:

- exact service calls
- gRPC request/response contracts
- state transitions
- database ownership
- transaction boundaries
- Kafka events
- retry rules
- idempotency strategy
- timeout/deadline propagation
- compensation behavior
