# Customer-Approved Quote Flow

## Purpose

This document refines the Booking/Pricing Saga design so a customer explicitly receives and accepts an exact price before `CreateBooking` can trigger inventory reservation or payment.

It supplements `docs/booking-payment-recovery-design.md` and supersedes any interpretation that `CreateBooking` itself should silently obtain a new customer price immediately before charging.

## Public flow

```text
Search
  ↓
estimated price only
  ↓
customer selects room
  ↓
POST /api/v1/quotes
  ↓
exact immutable quote + quote_id + expires_at
  ↓
customer confirms
  ↓
POST /api/v1/bookings { quote_id, payment_method_id }
```

Search prices are advisory. The exact quote is the customer-facing price commitment for a short bounded time.

## Quote ownership

Pricing Service owns durable quotes.

A quote contains immutable booking inputs and amounts:

```text
id
hotel_id
room_type_id
check_in
check_out
guest_count
room_quantity
subtotal
tax
service_fee
discount
total
currency
pricing_version
expires_at
created_at
```

All monetary values use integer minor units.

A quote must never be updated to a new amount. Changed pricing requires creation of a new quote.

## CreateBooking contract

The public request should reference an accepted quote rather than client-supplied money:

```json
{
  "quote_id": "quote_001",
  "payment_method_id": "pm_123"
}
```

The external `Idempotency-Key` header is still required and is owned by Booking Service semantics.

## Booking validation

Before reserving inventory or starting payment, Booking Service obtains the authoritative quote through its `PricingRepository` boundary and verifies:

- quote exists
- quote is not expired
- quote is structurally valid
- quote belongs to the requested customer flow when any ownership binding is introduced
- all immutable stay/room inputs come from the quote, not mutable client fields

If the quote is expired, Booking returns `QUOTE_EXPIRED` and performs no inventory or payment side effect.

## Price snapshot

After accepting the quote, Booking persists an immutable local price snapshot before the first irreversible remote step.

```text
GetQuote(quote_id)
  ↓
validate quote
  ↓
local transaction:
  booking created
  booking price snapshot persisted
  saga state persisted
  ↓
ReserveInventory
```

Payment always charges the persisted Booking snapshot total. It must not re-quote.

## Saga state refinement

The earlier `PRICE_QUOTED` Saga state should be interpreted as an accepted/validated customer quote. Implementations may name the state `PRICE_ACCEPTED` if schema/code has not yet stabilized.

Recommended sequence:

```text
STARTED
  ↓
PRICE_ACCEPTED
  ↓
INVENTORY_RESERVED
  ↓
PAYMENT_PROCESSING
  ...
```

The important invariant is semantic, not the exact enum spelling:

> No inventory reservation or payment starts before an unexpired customer-approved quote has been durably snapshotted by Booking.

## Search pricing

Search must not persist durable quotes for every result. Search can use batch pricing estimates.

```text
Search results
  → estimated_price

Selected room
  → exact durable quote

Booking
  → accepted quote snapshot
```

This avoids creating large numbers of unused quote rows.

## Failure semantics

`GetQuote` is a read/idempotent operation and can use bounded retry for transient failures.

`QUOTE_EXPIRED` is non-retryable inside the same booking attempt; the client must obtain a new quote.

A transient Pricing outage before Booking accepts the quote must not reserve inventory or charge payment.

## Clean Architecture boundary

Booking usecases depend only on a Pricing repository/gateway interface such as:

```go
type PricingRepository interface {
    GetQuote(ctx context.Context, quoteID string) (Quote, error)
}
```

The concrete gRPC client lives in infrastructure. Usecases must not depend on generated protobuf types.

## Architectural decision

The customer-approved quote is now part of the baseline Booking architecture. Coding PRs should implement this contract rather than reintroducing a silent re-quote during `CreateBooking`.