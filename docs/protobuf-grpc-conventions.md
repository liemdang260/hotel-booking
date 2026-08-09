# Protobuf and gRPC conventions

## Layout and generation

Source contracts live under `proto/<area>/v1`. Generated Go code lives only under `gen/go`; generated messages and stubs must not be placed in `internal/domain` or `internal/usecase`.

Run:

```bash
buf lint
buf generate
sh scripts/proto-smoke.sh
```

`buf.gen.yaml` uses source-relative output, so `proto/smoke/v1/smoke.proto` generates into `gen/go/smoke/v1`.

## Package and version naming

- Proto packages use `hotelbooking.<area>.v1`.
- Files are grouped under an explicit API major version directory such as `proto/booking/v1`.
- Go package options point to `github.com/liemdang260/hotel-booking/gen/go/<area>/v1`.
- Backward-compatible changes stay within the current version. Breaking wire/API changes require a new version namespace.

## Clean Architecture boundary

Protobuf types are transport types. Application adapters translate protobuf requests into usecase input types and translate usecase results back into protobuf responses. Domain and usecase packages must not import generated protobuf packages. Infrastructure gRPC clients may use generated protobuf types but expose inner repository/gateway interfaces to usecases.

## Errors

Use canonical gRPC status codes for the high-level failure category. Add stable machine-readable detail when callers need structured context. `hotelbooking.common.v1.ErrorDetail` defines `reason`, `request_id`, and metadata fields. Do not expose database errors, stack traces, credentials, or other implementation details to clients.

Expected mappings include invalid input to `InvalidArgument`, missing resources to `NotFound`, concurrency/precondition failures to `FailedPrecondition` or `Aborted`, authentication failures to `Unauthenticated`, authorization failures to `PermissionDenied`, and unexpected failures to `Internal`.

## Deadlines, cancellation, and retries

- Every outbound gRPC request must use the caller's context so cancellation and deadlines propagate; do not replace it with `context.Background()` inside request flows.
- Public entry points establish an explicit request deadline when the caller did not provide a suitable one.
- Retry only transient failures and only when the operation is read-only or explicitly idempotent.
- Do not automatically retry validation, authentication, authorization, or deterministic business errors.
- Retried mutations require a stable idempotency key and bounded backoff; retries must never extend beyond the caller deadline.

## Smoke contract

`proto/smoke/v1/smoke.proto` is intentionally minimal. The smoke script lints the workspace, regenerates Go output, and asserts that message, gRPC stub, and common error-detail files are produced in the isolated `gen/go` tree.
