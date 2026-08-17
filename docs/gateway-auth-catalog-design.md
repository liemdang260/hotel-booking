# API Gateway, Auth, Catalog, Search, and Quote Boundary Design

## Purpose

This document defines the client-facing architecture of the hotel booking platform. It establishes API Gateway responsibilities, authentication/authorization boundaries, Catalog ownership, search composition, exact quote semantics, public error handling, and how these concerns fit the project Clean Architecture convention.

The goal is to keep clients independent from the internal microservice topology while preserving service ownership and booking correctness.

## 1. Public system boundary

External clients communicate only with API Gateway over HTTPS/REST.

```text
Web / Mobile
    |
  HTTPS
    v
API Gateway
    |
    +--> Auth
    +--> Catalog
    +--> Availability
    +--> Pricing
    +--> Booking
```

Internal services are not exposed as public REST APIs.

Gateway responsibilities:

- HTTP termination and public API versioning
- authentication
- coarse rate limiting
- request validation and mapping
- correlation/request IDs
- end-to-end deadline propagation
- public error normalization
- query composition for read paths

Gateway must not own:

- booking state machines
- inventory invariants
- pricing rules
- payment rules
- Saga decisions

## 2. Gateway Clean Architecture

Gateway follows the same dependency rule as every service:

```text
HTTP Application entry point
        |
        v
      Usecase
        |
        v
Repository / gateway interfaces
        |
        v
Infrastructure gRPC / cache adapters
```

HTTP handlers map transport data into usecase input and map usecase output/errors back into public HTTP responses. They must not contain service orchestration decisions beyond invoking the relevant usecase.

Suggested structure:

```text
services/gateway/internal/
  application/http/
  usecase/
  domain/repository/
  infrastructure/grpc/
  infrastructure/redis/
  infrastructure/auth/
```

## 3. Auth Service ownership

Auth Service owns user credential and token lifecycle data.

Core tables:

```text
users
  id
  email UNIQUE
  password_hash
  status
  created_at
  updated_at

refresh_tokens
  id
  user_id
  token_hash
  expires_at
  revoked_at
  rotated_from
  created_at
```

Refresh tokens are stored as hashes, never plaintext.

Auth does not own Booking authorization rules such as whether a user may access a particular booking.

## 4. Access and refresh token model

Baseline model:

- short-lived JWT access token
- longer-lived opaque refresh token
- refresh-token rotation

Example access token lifetime: approximately 15 minutes.
Example refresh token lifetime: approximately 30 days.

JWT claims should remain minimal and stable:

```json
{
  "sub": "user_123",
  "iss": "hotel-booking-auth",
  "aud": "hotel-booking-api",
  "iat": 0,
  "exp": 0,
  "roles": ["customer"]
}
```

`sub` is the authoritative user identity. Mutable fields such as email/name are not authorization identities.

## 5. Local token validation at Gateway

Gateway should not call Auth Service for every authenticated request.

Auth signs access tokens with an asymmetric key. Gateway verifies tokens locally using the corresponding public key/JWKS material.

```text
Auth private signing key
        |
        v
       JWT
        |
        v
Gateway local verification
```

This removes Auth from the request hot path.

## 6. Refresh-token rotation

Refresh flow:

```text
R1 presented
  -> validate stored hash
  -> revoke R1
  -> issue R2
  -> issue new access token
```

The schema should allow token-family/reuse detection later. MVP may implement a simpler subset, but the storage model must not prevent safe rotation.

## 7. Authorization boundary

Authentication may happen at Gateway, but business authorization belongs to the service that owns the data/invariant.

Example:

```text
GET /api/v1/bookings/B123
Gateway authenticates actor U1
Booking Service verifies booking.user_id == U1
```

This remains correct even when future internal callers bypass the public Gateway.

Transport identity must be mapped into explicit usecase input. Booking usecases must not parse JWT or gRPC metadata directly.

## 8. Internal service authentication

For MVP, services remain private to the internal network and trust the platform service boundary plus propagated actor identity.

Production hardening may add mTLS/service identity. The architecture does not require per-request calls to Auth Service or bespoke API keys between every service.

## 9. Catalog Service ownership

Catalog owns descriptive hotel and room-type metadata:

- Hotel name and description
- address/geolocation
- amenities
- photos
- RoomType name/description
- bed/capacity metadata
- active/inactive catalog state

Catalog does not own:

- remaining inventory
- reservations
- nightly price/rate
- bookings
- payments

Service boundaries:

```text
Catalog       -> metadata
Availability  -> inventory / holds
Pricing       -> rates / quotes
Booking       -> customer transaction / Saga
```

## 10. Catalog persistence

Typical Catalog tables:

```text
hotels
room_types
hotel_amenities
room_type_amenities
hotel_images
```

Foreign keys within Catalog DB are normal and encouraged.

Other services may store `hotel_id` and `room_type_id` as opaque references, but must not create cross-service foreign keys or query Catalog tables directly.

## 11. Search as query composition

MVP does not require a dedicated Search Service.

Public search request:

```http
GET /api/v1/hotels/search?city=Tokyo&check_in=2026-09-01&check_out=2026-09-04&guests=2&rooms=1
```

Gateway `SearchHotelsUsecase` composes read data:

```text
Catalog.Search candidates
        |
        +--> Availability.BatchCheck
        +--> Pricing.BatchEstimate
        |
        v
compose public response
```

This is a read/query composition only. It does not create booking guarantees.

## 12. Batch contracts and N+1 avoidance

Search must not issue one RPC per room type.

Bad:

```text
50 room types
  -> 50 Availability RPCs
  -> 50 Pricing RPCs
```

Required direction:

- Catalog returns a bounded candidate set
- one batch Availability call
- one batch Pricing estimate call

Conceptual RPCs:

```text
BatchCheckAvailability
BatchEstimate
```

Candidate sets must be bounded before fanout. Initial limits can be conservative and later tuned by benchmark.

## 13. Search consistency model

Search availability and prices are advisory.

A room visible in search may sell out before reservation. That is expected concurrent behavior, not an invariant violation.

Authoritative checks happen later:

```text
Search availability -> advisory
ReserveInventory     -> authoritative

Search estimate      -> advisory
Exact Quote          -> authoritative customer price for bounded TTL
```

## 14. Exact customer-approved quote

Search should not silently become the financial commitment.

Recommended public flow:

```text
Search
  -> estimated price

Customer selects room
  -> POST /api/v1/quotes
  -> exact immutable quote

Customer confirms
  -> POST /api/v1/bookings with quote_id
```

This ensures the customer sees the amount before payment is attempted.

## 15. Quote API

Conceptual endpoint:

```http
POST /api/v1/quotes
```

Request:

```json
{
  "hotel_id": "hotel_001",
  "room_type_id": "deluxe",
  "check_in": "2026-09-01",
  "check_out": "2026-09-04",
  "rooms": 1,
  "guests": 2
}
```

Response:

```json
{
  "quote_id": "quote_001",
  "currency": "USD",
  "subtotal": 30000,
  "tax": 3000,
  "service_fee": 1000,
  "discount": 0,
  "total": 34000,
  "expires_at": "2026-09-01T12:05:00Z"
}
```

All amounts use integer minor units.

## 16. Quote persistence and immutability

Pricing Service owns durable exact quotes.

Typical fields:

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

Quotes are immutable. Changed rates produce a new quote rather than updating an accepted quote.

Search should not persist a durable quote for every result. Exact quotes are created only after user selection.

## 17. Refined CreateBooking contract

Public CreateBooking should accept:

```json
{
  "quote_id": "quote_001",
  "payment_method_id": "pm_123"
}
```

and require an external `Idempotency-Key` header.

Gateway forwards that key unchanged. Booking Service owns its persistence and conflict semantics.

Booking validates the quote through its Pricing repository/gateway boundary, persists an immutable local price snapshot, and only then starts inventory/payment side effects.

Expired quotes return `QUOTE_EXPIRED`; no inventory or payment action is allowed for that attempt.

## 18. Gateway does not own booking idempotency

Booking idempotency must remain correct across multiple Gateway replicas and retries.

Therefore Gateway may validate presence/format of `Idempotency-Key`, but the actual uniqueness, request hash, existing result, and conflict behavior live in Booking Service.

## 19. Public error model

Gateway normalizes internal errors into stable public responses.

Example:

```json
{
  "error": {
    "code": "ROOM_SOLD_OUT",
    "message": "The selected room is no longer available.",
    "request_id": "req_123",
    "details": {}
  }
}
```

Conceptual mapping:

```text
invalid argument        -> 400
unauthenticated         -> 401
permission denied       -> 403
not found               -> 404
idempotency conflict    -> 409
room sold out           -> 409
quote expired           -> 409
dependency unavailable  -> 503
deadline exceeded       -> 504
internal                -> 500
rate limit              -> 429
```

The public client must not depend on internal gRPC status text.

## 20. Search failure policy

Initial MVP should fail the request clearly if a required search dependency is unavailable rather than returning undocumented partial data.

Graceful degradation can be introduced later only with an explicit public contract and UI semantics.

## 21. Deadline propagation

Gateway owns the public request deadline budget and passes the upstream context into all downstream repository/gRPC calls.

No layer may replace the request context with `context.Background()` for request-bound work.

Downstream calls must not outlive the upstream deadline.

## 22. Rate limiting and Redis

Gateway may use Redis for non-authoritative concerns such as rate-limit counters.

Examples:

- stricter per-IP limits for login
- per-user mutation limits for booking
- higher limits for search reads

Redis must not become a source of truth for booking correctness.

## 23. Catalog caching

Catalog metadata is a good caching candidate. Catalog Service may own Redis caching for hotel/room metadata.

Gateway should not independently cache Catalog domain objects in a way that weakens ownership boundaries.

Availability read caching may be introduced later, but `ReserveInventory` remains PostgreSQL-authoritative.

## 24. Booking detail composition

Booking Service returns transaction-owned data such as booking status, dates, price snapshot and opaque catalog IDs.

If the public response needs live hotel/room display metadata, Gateway may compose Booking + Catalog reads rather than joining databases.

Historical name/photo fidelity is optional for MVP. Financial price fidelity is mandatory and handled by Booking's immutable price snapshot.

## 25. Search Service is deferred

Do not add Elasticsearch/OpenSearch or a dedicated Search Service purely for architectural appearance.

Gateway batch composition is sufficient initially.

A dedicated search projection becomes justified only after metrics show the fanout/read path is a meaningful bottleneck or search traffic requires independent scaling.

Future form:

```text
Catalog events ------+
Availability events -+--> Search Projection --> OpenSearch --> Gateway
Pricing events ------+
```

Booking would still revalidate exact quote and inventory, so eventual search consistency remains safe.

## 26. Public API baseline

Auth:

```text
POST /api/v1/auth/register
POST /api/v1/auth/login
POST /api/v1/auth/refresh
POST /api/v1/auth/logout
```

Catalog/Search/Pricing:

```text
GET  /api/v1/hotels/search
GET  /api/v1/hotels/{hotel_id}
GET  /api/v1/hotels/{hotel_id}/room-types
POST /api/v1/quotes
```

Booking:

```text
POST /api/v1/bookings
GET  /api/v1/bookings/{booking_id}
GET  /api/v1/bookings
POST /api/v1/bookings/{booking_id}/cancel
```

Payment Service has no direct public client endpoint in the booking workflow.

## 27. Baseline architectural decisions

1. API Gateway is the only public service boundary.
2. Gateway handlers are Application entry points; business/query orchestration belongs in Gateway usecases.
3. Auth uses short-lived JWT access tokens plus rotatable opaque refresh tokens.
4. Gateway verifies JWT locally instead of calling Auth on every request.
5. Owning services enforce resource authorization.
6. Catalog owns metadata; Availability owns inventory; Pricing owns rates/quotes; Booking owns the transaction.
7. MVP search uses Gateway query composition with bounded batch RPCs.
8. Search availability/pricing is advisory.
9. Exact immutable customer-approved quote precedes CreateBooking.
10. CreateBooking references `quote_id` and Booking persists the quote snapshot before side effects.
11. Gateway does not own Booking idempotency state.
12. Redis is not authoritative for booking correctness.
13. Dedicated Search Service/OpenSearch is deferred until metrics justify it.

Implementation PRs should follow these decisions and raise architecture changes separately rather than redesigning boundaries inside coding tasks.