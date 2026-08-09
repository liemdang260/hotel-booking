# Security and Trust Boundaries Design

## Purpose

This document defines the security model and trust boundaries for the hotel booking platform. The goal is to make identity, authorization, sensitive-data ownership, internal-service trust, auditability, and operator access explicit instead of assuming that a private network or an API Gateway makes the system trusted by default.

This is a platform architecture document. Individual implementation tickets should apply these decisions without moving security-sensitive business rules into transport middleware or bypassing service ownership.

Clean Architecture remains mandatory:

```text
Application entry point
        ↓
Usecase
        ↓
Repository / policy interfaces
        ↓
Infrastructure adapters
```

Authentication parsing/verification is primarily an Application/infrastructure concern. Authorization decisions that depend on business ownership belong in the owning service usecase/domain.

---

## 1. Threat/trust model

The architecture assumes the following actors can be untrusted or compromised independently:

```text
Internet client
browser/mobile application
malformed or replayed HTTP requests
stolen/expired user token
one compromised service instance
misconfigured internal caller
Kafka message replay/duplicate
operator/admin credentials
third-party payment/notification provider
logs/traces/metrics as potential data-exfiltration surfaces
```

A private VPC/Kubernetes network is not treated as sufficient authentication by itself for production.

Primary trust boundaries:

```text
Internet
   │
   ▼
API Gateway
   │ authenticated user/service context
   ▼
Internal services
   │
   ├── service-owned PostgreSQL
   ├── Kafka
   └── external providers
```

Every crossing must define who authenticated the caller, what identity is propagated, what data is allowed, and who performs authorization.

---

## 2. Public exposure model

Only the API Gateway is publicly reachable for customer APIs.

Do not expose directly to the Internet:

```text
Booking gRPC
Availability gRPC
Pricing gRPC
Payment gRPC
Catalog internal gRPC
Notification workers
PostgreSQL
Redis
Kafka
Prometheus/internal debug endpoints
```

Auth public endpoints are still reached through Gateway even if Auth is a separate internal service.

Network policy/security groups are defense in depth, not the only identity control.

---

## 3. Client-controlled headers are untrusted

Internet clients may send arbitrary headers including names that look internal, for example:

```text
X-User-ID
X-Roles
X-Service-Name
X-Correlation-ID
X-Forwarded-For
traceparent
```

Gateway must maintain an allowlist/normalization policy.

Security-sensitive identity headers from the public request must be removed/overwritten before internal forwarding.

Rule:

> A downstream service must never trust a user identity merely because a public client supplied a header with an internal-looking name.

Gateway creates trusted internal principal metadata only after successful authentication.

---

## 4. User authentication model

Customer authentication uses:

```text
short-lived signed access token (JWT)
+
longer-lived opaque refresh token
```

Access token contains a minimal identity/authorization envelope such as:

```text
sub = immutable user_id
iss = expected issuer
aud = expected API audience
iat / exp
roles/scopes only if stable and required
jti optional where operationally useful
```

Do not use mutable fields such as email/display name as the authoritative resource identity.

---

## 5. JWT verification

Gateway verifies access tokens locally using an asymmetric public key/JWKS set.

Verification must validate at least:

```text
signature
accepted algorithm/key type
issuer
audience
expiration/not-before where used
key identifier lookup where used
```

The implementation must not accept `alg=none`, dynamically trust arbitrary algorithms from the token, or fetch keys from attacker-controlled token-provided URLs.

JWT parsing libraries are infrastructure/Application concerns; inner usecases receive a trusted Principal value, not raw JWT strings.

---

## 6. JWT signing-key rotation

Auth signing keys require rotation without invalidating all currently valid access tokens immediately.

Recommended model:

```text
active signing key K2
previous verification key K1 retained during overlap
JWKS/public-key set contains K1 + K2 during transition
new tokens use K2
old K1 tokens remain valid until their short TTL expires
K1 removed after overlap window
```

Tokens identify signing key using a stable key ID (`kid`).

Gateway caches public verification keys but refreshes the trusted key set when it encounters an unknown valid-looking `kid` and according to a bounded refresh policy.

Failure to refresh keys must fail closed for authentication rather than accept unverifiable tokens.

Private signing keys exist only in Auth/key-management infrastructure and are never distributed to Gateway or other services.

---

## 7. Refresh-token model

Refresh tokens are opaque random values and are never stored plaintext in Auth DB.

Persist a one-way hash plus lifecycle metadata:

```text
id
user_id
token_hash
family/session_id
expires_at
revoked_at
rotated_to / rotated_from
created_at
last_used_at (optional)
```

Refresh semantics:

```text
R1 presented
    ↓
validate hash + status
    ↓
revoke/rotate R1
    ↓
issue R2 + new access token
```

Reuse of an already-rotated token is treated as suspicious. A simple MVP may revoke the associated token family/session and require re-authentication.

Refresh-token values must not appear in logs, traces, metrics, Jira comments, or Kafka events.

---

## 8. Password/credential boundary

Auth is the only service that owns password credentials.

Requirements:

```text
modern password hashing through a dedicated password-hasher interface
per-password salts as provided by the chosen algorithm/library
constant-time comparison where applicable
no plaintext/reversible password storage
no password logging
```

Password policy/rate limits should prevent trivial abuse without relying on arbitrary complexity rules as the only defense.

Login endpoints receive stronger rate limiting than ordinary read APIs.

Account enumeration should be minimized in public error responses: invalid email and invalid password should not needlessly reveal different externally observable details.

---

## 9. Authentication vs authorization

Gateway authenticates the user.

Owning services authorize access to their resources.

Example:

```text
GET /bookings/B123
Authorization: Bearer <U1 token>
```

Gateway establishes:

```text
Principal.UserID = U1
```

Booking Service still enforces:

```text
booking.user_id == U1
```

unless an explicit privileged internal/admin capability is used.

Do not rely solely on Gateway route protection for ownership checks.

---

## 10. Principal representation

Inner code uses a transport-independent principal:

```go
type Principal struct {
    UserID  string
    Roles   []string
    SubjectType SubjectType
}
```

Application layer maps verified token/internal identity into this type.

Usecase inputs contain required actor identity explicitly, for example:

```go
type GetBookingInput struct {
    Actor     Principal
    BookingID string
}
```

Usecases must not read raw HTTP headers, JWT claims, or gRPC metadata directly.

---

## 11. Role vs resource authorization

Roles answer broad capability questions:

```text
customer
support
admin
```

Resource ownership answers object-specific questions:

```text
Does U1 own Booking B123?
```

Both may be required.

Examples:

```text
customer + owns booking
    -> may view/cancel according to policy

support role
    -> may inspect booking through explicit support capability

admin role
    -> does not automatically justify arbitrary cross-service DB access
```

Privileged actions must be explicit usecases with audit records.

---

## 12. Internal user-identity propagation

Gateway forwards a normalized principal to internal services through trusted internal metadata or dedicated request fields.

Preferred principle:

> Business-relevant identity should appear in the internal application contract rather than be hidden in ambient transport state wherever practical.

For example Booking CreateBooking request can include an authenticated actor/user identity populated by Gateway, while the internal transport also carries service identity for caller authentication.

Downstream services must distinguish:

```text
end-user identity
vs
calling-service identity
```

They are not the same thing.

---

## 13. Service-to-service identity

MVP local development can run on a trusted Docker network for convenience, but production design must not equate network location with identity.

Target production model:

```text
mTLS / workload identity
```

or an equivalent orchestrator/service-mesh identity mechanism.

Each service receives an identity such as:

```text
gateway
booking
availability
pricing
payment
notification
```

Internal server interceptors authenticate the calling workload before accepting privileged internal RPCs.

The exact mTLS/service-mesh technology is deployment-specific and can be added in production hardening without changing usecase/domain APIs.

---

## 14. Service authorization

Authenticating that a caller is `booking-service` does not imply it may call every internal API.

Define least-privilege service capabilities conceptually:

```text
Gateway -> Booking: create/query/cancel customer bookings
Gateway -> Catalog: search/read catalog
Gateway -> Pricing: quote/estimate
Booking -> Availability: reserve/confirm/release/cancel reservation
Booking -> Payment: create/get payment, create/get refund
Booking -> Pricing: get/validate accepted quote
Notification -> no Booking DB access
```

Infrastructure/interceptor policy may enforce coarse service-to-service permissions. Business-sensitive rules still live in owning usecases.

---

## 15. Do not forward public Authorization token blindly everywhere

The customer bearer token should not automatically be forwarded through every internal hop as the only security mechanism.

Preferred separation:

```text
Client -> Gateway
  user JWT authenticates user

Gateway -> Booking
  workload/service identity authenticates Gateway
  normalized end-user Principal is propagated as application context
```

This prevents every service from needing to become a public JWT trust endpoint and keeps internal service authorization explicit.

A production design may support end-user token forwarding for specific standards/infrastructure, but it must be intentional rather than accidental header passthrough.

---

## 16. Authorization for asynchronous events

Kafka events are not user requests and should not be authorized by replaying a user JWT.

Events contain business/audit identity where needed:

```text
actor_user_id
correlation_id
causation_id
```

but consumers trust the event channel/producer identity + schema, not a customer bearer token embedded in the payload.

Never publish access/refresh tokens into Kafka.

---

## 17. Payment data boundary

The platform should minimize payment-sensitive data.

Booking/Gateway should handle only provider/payment-method references such as:

```text
payment_method_id / tokenized provider reference
```

Do not persist or log raw card PAN, CVV, magnetic-stripe data, or equivalent raw payment credentials.

Payment Service is the only service that communicates with the payment provider adapter for charge/refund operations.

Booking stores Payment IDs/status summaries and financial snapshots required for its workflow, not provider secrets/card credentials.

---

## 18. Payment provider credentials

Provider API keys/webhook secrets/private credentials belong to Payment infrastructure configuration only.

They must not be:

```text
committed to git
stored in Booking DB
included in Kafka events
returned via gRPC/public API
logged/traced
```

Credential rotation should be possible through deployment secret configuration without code changes.

---

## 19. Future payment webhooks

If provider webhooks are added later, they terminate at Payment Service (or a dedicated Payment Application endpoint), not Booking.

Webhook security must include provider-authenticity verification using the provider's supported signature/authentication mechanism before processing.

Webhook `provider_event_id` is stored uniquely for deduplication.

Webhook handler:

```text
verify authenticity
    ↓
Application handler
    ↓
Payment reconciliation usecase
    ↓
Payment DB/outbox
```

No direct provider webhook mutation of Booking DB.

---

## 20. PII ownership/minimization

Collect and propagate only data needed for each usecase.

Examples:

```text
Auth owns email/login profile needed for identity
Booking owns user_id and booking business data
Notification may own delivery recipient snapshot needed to send a message
Analytics receives minimized business/event data, not credentials
```

Do not copy full user records into every service "for convenience".

Integration events should exclude secrets and unnecessary personal data.

Where Notification needs an email address, the architecture must deliberately choose either a minimal recipient snapshot or an explicit user/contact lookup; it must never query Auth's database directly.

---

## 21. Logs, traces and metrics are security boundaries

Observability data may be broadly accessible to engineering/operations and retained longer than request memory.

Never include:

```text
passwords
JWT/access tokens
refresh tokens
payment provider secrets
raw payment credentials
full Authorization headers
session cookies
sensitive webhook signatures
```

Avoid unnecessary PII such as full email addresses in generic logs/metric labels.

Trace/span attributes and structured error metadata follow the same redaction rules as logs.

Metrics must never use user_id, booking_id, token IDs, email, etc. as unbounded labels.

---

## 22. Request/response logging

Do not enable indiscriminate full HTTP/gRPC body logging in production.

Recommended logging is structured metadata:

```text
request_id
trace_id
service
operation
status/error_code
latency
authenticated subject type/id when appropriate and non-sensitive
business aggregate IDs when useful
```

For debugging payloads, use targeted development-only tooling with explicit redaction and retention controls.

---

## 23. Public error handling

Public errors must not leak:

```text
SQL text
stack traces
internal hostnames
provider credentials
raw provider error bodies
private table/schema details
JWT verification internals
```

Gateway maps internal structured errors to stable public codes.

Internal logs may record richer technical context after redaction.

Use correlation/request IDs so support can connect the public error to internal diagnostics without exposing internals to the client.

---

## 24. Request size and parsing limits

Gateway and public handlers must bound attacker-controlled input before expensive work.

Apply limits for:

```text
HTTP body size
header size/count
query-string size
JSON nesting/field validation where relevant
list/page size
search candidate size
string lengths
```

Reject oversized/invalid requests before fanout to downstream services.

Internal gRPC message sizes should also have documented/configured bounds; batch APIs remain bounded.

---

## 25. CORS and browser boundary

If a browser frontend is used, Gateway owns CORS policy.

Production CORS must use an explicit allowlist of known frontend origins and permitted methods/headers.

Do not combine credentialed requests with wildcard origins.

CORS is a browser policy and is not an authorization mechanism; server-side authentication/authorization remains mandatory.

---

## 26. CSRF considerations

If access credentials are sent via Authorization bearer headers and refresh tokens are managed through a secure client flow, CSRF exposure differs from cookie-authenticated APIs.

If refresh/access tokens are ever placed in cookies, cookie attributes and CSRF protections must be explicitly designed (`Secure`, `HttpOnly`, appropriate SameSite and anti-CSRF strategy where required).

Do not silently switch between header-token and cookie-token models without an ADR because browser threat assumptions change.

---

## 27. Rate limiting and abuse controls

Gateway rate limiting is defense in depth and is separate from business idempotency.

Examples:

```text
/login and /refresh
    strong per-IP/account/session protection

/search
    higher read quota and candidate limits

/quotes
    per-user/IP limit to prevent expensive quote spam

/bookings and /cancel
    per-user limits + business idempotency
```

Rate limit keys should avoid exposing raw sensitive data in Redis/logs where hashing/normalization is sufficient.

Rate-limit failure does not weaken Booking/Payment/Availability correctness.

---

## 28. Idempotency keys are not authentication tokens

`Idempotency-Key` protects duplicate mutation semantics but grants no authority.

A caller must still be authenticated/authorized.

Idempotency lookup is scoped to the relevant actor/resource boundary so one user's guessed key cannot retrieve another user's operation result.

Request hashes should cover all immutable business inputs that define equivalence.

Do not log high-volume arbitrary client idempotency values unnecessarily.

---

## 29. Object-level authorization examples

### Booking query

```text
customer may read booking when booking.user_id == principal.user_id
```

### Booking cancellation

```text
customer may request cancellation only for owned booking
then normal cancellation policy applies
```

### Admin inventory changes

Not part of customer API. Requires explicit privileged admin capability and audit record.

### Payment query

Payment is internal for MVP; customer cannot directly enumerate payment IDs through Payment Service.

---

## 30. Mass-assignment prevention

Public request DTOs expose only customer-controlled fields.

Do not decode arbitrary JSON directly into persistence/domain structs containing privileged fields such as:

```text
user_id
booking status
total amount
refund amount
roles
payment status
created_at
```

Application mapping constructs trusted usecase input from:

```text
validated public DTO
+
authenticated Principal
+
server-owned values
```

---

## 31. Admin/operator boundary

Operational recovery is powerful and must not be hidden inside ordinary customer endpoints.

Future privileged commands may include:

```text
ReconcilePayment
ReconcileRefund
RetryCancellation
RequeueOutboxEvent
InspectSaga
```

They should live behind a dedicated admin/operator Application surface with:

```text
strong service/user authentication
explicit privileged authorization
audit record
reason/ticket reference when appropriate
idempotent usecase invocation
```

No public route should accept an `is_admin=true` field from the client.

---

## 32. Break-glass access

Direct production DB writes across services are not a normal recovery mechanism.

Break-glass access, if required operationally, should be:

```text
rare
restricted
time-bound where possible
audited
performed from documented runbook
```

Preferred recovery always goes through service-owned reconciliation/usecases that preserve invariants.

---

## 33. Audit event model

Security/business-sensitive actions should create immutable audit records/events distinct from ordinary application logs.

Candidates:

```text
login success/failure patterns
refresh-token rotation/reuse detection
privileged operator action
booking cancellation
refund initiation/completion
manual Saga/payment/refund reconciliation
role/account-state changes
```

Audit record fields may include:

```text
audit_id
occurred_at
actor_type
actor_id
action
resource_type
resource_id
result
reason/correlation_id
sanitized metadata
```

Audit records must never contain credentials/secrets.

---

## 34. Audit ownership

The service that executes a security/business-sensitive action owns producing its audit fact.

It can persist/publish via the existing local Transactional Outbox so audit delivery does not create a dual write.

A downstream Audit consumer may build a central immutable/searchable audit store.

This does not allow the Audit service to query private service databases.

---

## 35. Kafka producer/consumer trust

Kafka's at-least-once/idempotency model remains, but security adds producer/consumer authorization.

Production broker policy should enforce topic-level least privilege conceptually:

```text
Booking may write booking.events
Availability may write availability.events
Payment may write payment.events
Notification may consume selected event topics
```

A consumer must still validate event schema/version and not treat arbitrary payload data as trusted executable instructions.

Never place customer bearer tokens/provider secrets in event headers or payloads.

---

## 36. Event actor/correlation data

Integration events may carry non-secret actor context such as:

```text
actor_user_id
actor_type
correlation_id
causation_id
```

for audit/debugging.

This metadata describes the business cause. It is not reused as an authentication credential by the consumer.

---

## 37. Database credentials and service ownership

Each service uses credentials limited to its own logical database/schema in production.

Example:

```text
booking-service DB user
    -> booking_db only

availability-service DB user
    -> availability_db only

payment-service DB user
    -> payment_db only
```

Do not give every microservice a PostgreSQL superuser/shared application credential.

This enforces the architecture's no-cross-service-query rule at infrastructure level as defense in depth.

Migration credentials may have broader DDL rights than runtime credentials and should be separated where practical.

---

## 38. Redis trust boundary

Redis is non-authoritative for this platform.

It may store:

```text
rate-limit counters
Catalog/read caches
other explicitly disposable cached data
```

Do not store plaintext access/refresh tokens or raw payment credentials unnecessarily.

A Redis loss/flush must not break Booking/Inventory/Payment correctness.

---

## 39. File/object/upload boundary

The MVP does not require arbitrary user file upload.

If hotel images/admin uploads are introduced later, they require a separate design for:

```text
content type/size validation
object-store permissions
malware/content scanning as appropriate
signed upload/download URLs
metadata ownership
```

Do not add generic file-upload endpoints to Gateway without this threat model.

---

## 40. Dependency and supply-chain baseline

Implementation/deployment should include basic dependency/supply-chain hygiene:

```text
pinned/controlled Go module dependencies via go.mod/go.sum
reviewed container base images
CI vulnerability/dependency scanning where practical
no secrets embedded in images
minimal runtime image/user permissions
reproducible build instructions
```

Security scanning results should be triaged rather than automatically disabling correctness-critical tests or pinning unsafe versions without review.

---

## 41. Container/runtime privilege

Production containers should run with least privilege where practical:

```text
non-root runtime user
read-only filesystem where service permits
no privileged container mode
minimal Linux capabilities
explicit writable temp paths if needed
resource limits
```

Services do not require host Docker socket or Kubernetes admin credentials.

---

## 42. Secrets and configuration

Security extends the production reliability config rules:

- secrets injected separately from non-secret config
- secret values are redacted from startup/config dumps
- configuration error messages identify the missing setting name without printing secret content
- key/provider credential rotation does not require source-code modification
- local `.env` files remain ignored; `.env.example` contains only safe placeholders/development defaults

---

## 43. Internal error and panic handling

Application recovery middleware may convert panics/unexpected errors into stable internal responses while preserving trace/log correlation.

It must not return stack traces to clients.

Panic recovery does not silently continue a partially executed business workflow. Durable Saga/idempotency/reconciliation rules determine recovery after process failure.

---

## 44. Security testing layers

Required security-oriented tests include:

### Authentication

```text
invalid signature
wrong issuer/audience
expired token
unknown/rotated kid behavior
revoked/rotated refresh-token reuse
```

### Authorization

```text
user A cannot read/cancel user B booking
customer cannot call admin/operator capability
service identity not authorized for unrelated internal RPC
```

### Input boundary

```text
forged internal identity headers from Internet are overwritten/rejected
oversized body/header/list inputs are rejected
mass-assignment fields are ignored/rejected
```

### Sensitive data

```text
logs/traces do not contain Authorization/password/refresh token/payment secret
Kafka events do not contain credentials
```

### Operational security

```text
runtime DB credential cannot query another service DB
admin/recovery action creates audit record
```

---

## 45. Security failure behavior

Authentication failures fail closed:

```text
invalid/unverifiable access token -> 401
```

Authorization failures:

```text
authenticated but forbidden -> 403 or resource-hiding policy where deliberately chosen
```

Internal workload authentication failure:

```text
reject RPC before business usecase execution
```

Security-system transient failures must not be converted into successful anonymous access.

---

## 46. Security and availability tradeoff

Examples:

```text
JWKS refresh temporarily unavailable
```

Gateway may continue verifying tokens using still-valid cached trusted keys according to bounded cache policy, but it never accepts a token signed by an unknown unverified key merely to remain available.

```text
rate-limit Redis unavailable
```

Fail-open/fail-closed can vary by endpoint risk, but business authorization/idempotency remains intact either way.

```text
service identity verification unavailable/misconfigured
```

Production internal privileged calls fail closed rather than accept unauthenticated callers.

---

## 47. Initial security scope vs deferred work

MVP/portfolio must implement:

```text
Auth credential isolation
JWT verification + key rotation-compatible design
refresh rotation/revocation
Gateway identity-header sanitization
object-level Booking authorization
request/body/list limits
sensitive-log redaction
Payment token/reference-only boundary
service-owned DB credentials logically separated
basic audit facts for privileged/recovery/cancellation/refund actions
```

Production-hardening can implement/deepen:

```text
mTLS/workload identity enforcement
fine-grained service-to-service policy
central audit store
container/security policies
automated dependency/image scanning
advanced abuse controls
```

This sequencing avoids blocking core MVP code on a service mesh while keeping the trust model explicit from day one.

---

## 48. Baseline architecture decisions

Unless superseded by an ADR:

1. API Gateway is the only customer-facing network boundary.
2. Public client headers are untrusted; Gateway strips/overwrites security-sensitive internal identity headers.
3. Gateway authenticates users; owning services authorize resource access.
4. Inner usecases receive a transport-independent Principal, not raw JWT/HTTP/gRPC metadata.
5. Access JWTs are short-lived and asymmetrically signed; Gateway verifies locally.
6. JWT key rotation uses overlapping verification keys/JWKS and `kid`; private keys remain only in Auth/key infrastructure.
7. Refresh tokens are opaque, stored hashed, rotated and revocable; suspicious reuse can revoke the session/family.
8. Private network location alone is not sufficient production service identity; target production model is mTLS/workload identity or equivalent.
9. Calling-service identity and end-user identity are separate concepts.
10. Customer bearer tokens are not blindly forwarded through every internal hop as the sole trust mechanism.
11. Internal services follow least-privilege service capabilities.
12. Payment Service/provider adapter is the only boundary handling payment-provider credentials; raw card credentials are never stored/logged by this platform.
13. Observability/event pipelines are treated as sensitive-data boundaries and never carry credentials/secrets.
14. Public DTOs are explicitly mapped to usecase inputs to prevent mass assignment.
15. Idempotency keys do not grant authentication/authorization.
16. Admin/operator recovery uses dedicated privileged usecases with audit, not ordinary customer endpoints or routine direct SQL.
17. Service DB runtime credentials are scoped to service-owned data as defense in depth.
18. Kafka producer/consumer permissions are topic/service scoped in production; event actor metadata is not authentication credentials.
19. Authentication/workload-verification failures fail closed.
20. Security controls must preserve the existing business correctness/idempotency/Saga ownership model rather than bypass it.

---

## 49. Implementation consequences

Existing Auth/Gateway tasks should implement the customer identity and request-boundary decisions immediately.

Production-hardening tasks should add workload identity, internal authorization, audit hardening, secret/logging validation and container/deployment security.

If the backlog does not explicitly cover these cross-cutting controls, PM/BA should refine the relevant Auth/Gateway tasks and add one focused production security-hardening ticket before final deployment documentation.
