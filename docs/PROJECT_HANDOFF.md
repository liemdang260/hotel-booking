# Hotel Booking — Project Handoff

> Purpose: durable context for a new ChatGPT/agent session. This document summarizes the architecture, implementation rules, Jira/GitHub workflow, CI policy, and decisions established during the project. It is a handoff/index, not a replacement for specialized architecture documents.

## 1. Project goal

Build a portfolio-grade Go hotel-booking backend that demonstrates production-oriented backend engineering: microservices, gRPC, Clean Architecture, PostgreSQL, distributed workflows, idempotency, concurrency control, recovery, observability-ready boundaries, migrations, Docker-based runtime validation, and disciplined CI/review.

The project is intentionally designed beyond CRUD. Important behavior includes inventory reservation under concurrency, exact accepted pricing snapshots, payment uncertainty/reconciliation, booking saga recovery, cancellation/refund workflows, and immutable commercial terms.

## 2. Architectural principles

### Clean Architecture

Required dependency direction:

`application entrypoint -> usecase -> repository/domain ports -> infrastructure adapters`

Rules:
- `application` contains entrypoints/wiring only; business logic does not live there.
- Usecases orchestrate domain behavior and call repository/outbound ports.
- Domain/usecase layers must not import protobuf, PostgreSQL/pgx, generated gRPC, Docker, or infrastructure-specific types.
- gRPC/generated protobuf belongs in outer transport/adapters.
- PostgreSQL implementation belongs in infrastructure adapters.
- Transaction boundaries are explicit ports; remote calls must not occur inside a local DB transaction unless an approved design explicitly requires it.

### Architecture source-of-truth precedence

When documents/code disagree, use this order:
1. Current Jira acceptance criteria and architecture constraints.
2. Latest specialized architecture document/PR for that concern.
3. Architecture baseline/index in PR #20.
4. Older broad design documents.
5. Existing implementation only when it does not conflict with newer approved architecture.

Do not preserve an obsolete design just because code already implements it.

Known superseded concepts include:
- single `reserved_quantity` style inventory instead of the approved reservation model;
- treating payment timeout as definitive failure;
- client-controlled booking amount / re-pricing immediately before charge;
- using HELD reservation release semantics for BOOKED cancellation;
- representing refund only as `Payment.status = REFUNDED`.

## 3. Core domain decisions

### Availability

Inventory mutations must be concurrency-safe and idempotent. PostgreSQL integration tests are required for locking/transaction semantics that cannot be proven by fake repositories or SQL-string assertions.

Important tested behaviors include:
- concurrent reservation without oversell;
- rollback across multi-night reservation failures;
- optimistic version conflicts;
- expiration workers using locking / `SKIP LOCKED` without double-expiration;
- confirm-vs-expire races yielding one valid terminal outcome;
- inventory release and outbox persistence atomically where required.

### Pricing

Pricing owns commercial calculation. Quotes are exact accepted commercial snapshots, not hints that Booking later recalculates.

Money uses integer minor units.

Cancellation commercial terms are part of the accepted Quote. Hotel-local cancellation rules are resolved into an absolute instant before returning/persisting the quote.

Pricing Quote cancellation fields include concepts such as:
- policy code;
- policy version;
- resolved absolute `free_cancel_until`;
- refund basis points;
- cancellation fee in minor units.

Pricing Quote persistence is immutable and enforced/tested at the database layer.

### Booking

Booking owns orchestration/saga state and persists the exact accepted Pricing snapshot. It must not re-query current Pricing policy at cancellation time.

BookingStatus and SagaState are distinct concepts.

CreateBooking durable ordering follows the approved workflow: obtain quote, persist durable state/snapshot, reserve inventory, persist progress, persist payment-request intent/progress, call payment, persist outcome, confirm reservation, persist final progress. Remote calls are not hidden inside local DB transaction callbacks.

Idempotency must be integrated into the actual CreateBooking orchestration, not tested as an isolated helper. Replaying the same idempotency key/request must return/recover the existing booking without duplicate reserve/payment/confirm side effects.

Saga recovery must resume from durable state after restart and avoid duplicating side effects.

### Payment

Payment uncertainty is first-class:
- provider timeout/ambiguous result => `UNKNOWN`, not FAILED;
- reconciliation queries provider truth and resolves UNKNOWN to authoritative SUCCEEDED/FAILED;
- retry/replay with the same idempotency key must not create a second charge;
- payment attempt/audit persistence and state changes must be transactionally consistent where required;
- reconciliation work uses bounded retry/lease/reclaim semantics.

Refund is NOT `Payment.status = REFUNDED`.

The newer architecture treats refund as a first-class aggregate/workflow with its own persistence, attempts, idempotency and UNKNOWN reconciliation. Refund implementation belongs to the later refund tasks (not the old SCRUM-25 interpretation).

### Cancellation policy snapshot

SCRUM-36 introduced the accepted cancellation policy snapshot design:
- Pricing resolves/version-controls cancellation terms.
- Booking receives them through transport-independent outbound contracts.
- Booking persists an immutable cancellation snapshot with the accepted price snapshot.
- Existing bookings are unaffected by later policy changes.
- Accepted price and cancellation snapshot persistence share the Booking transaction abstraction and validate booking-ID consistency.

## 4. Jira sequencing

`seq-NNN` labels are the ONLY authoritative implementation ordering mechanism.

Ignore Jira `Blocks` / `is blocked by` links for eligibility. Historical links were duplicated/inconsistent in both directions.

Eligibility:
- `seq-001` has no predecessor.
- `seq-N` may start when immediate predecessor `seq-(N-1)` is **In Review or Done**.
- During this project phase, In Review intentionally unblocks the next task so development does not stall waiting for human merge/review.
- Do not require all earlier tasks to be Done.
- A worker may process at most three tasks per scheduled run.

The cancellation/refund sequence was updated to the newer design. SCRUM-25 (`seq-018`) was converted into a Payment charge/recovery integration gate; it does NOT implement refund. Current continuation is:

`SCRUM-25 seq-018 -> SCRUM-36 seq-019 -> SCRUM-37 seq-020 -> SCRUM-38 seq-021 -> later cancellation/refund tasks`

At handoff time SCRUM-36 is In Review with PR #75, so the next automation candidate should be `seq-020` if its Jira state/labels are eligible.

## 5. Stacked PR model

Implementation PRs are stacked, not all based on `main`.

Rules:
- seq-001: branch from latest `main`, PR base = `main`.
- If predecessor is In Review: create current task branch from predecessor PR HEAD and set current PR base to predecessor PR HEAD branch.
- If predecessor is Done/merged: branch from latest `main`, PR base = `main`.
- If predecessor is In Review but its unique open PR/head cannot be resolved: stop with `STACK_BASE_BLOCKED`.
- Never create an isolated child branch from `main` while its predecessor only exists in an unmerged PR.
- Never force-update unrelated task branches.

Internal stack-refresh merges are allowed only `predecessor branch -> child task branch`; they must never target `main`.

Why: early PRs were incorrectly isolated from `main`, so child code did not contain predecessor scaffold and CI could not compile/test the real stack. Those PRs were repaired into a stacked chain.

## 6. CI execution environment

Do not rely on the user's computer or a persistent ChatGPT Docker VM.

GitHub Actions is the canonical shared execution environment for interactive and scheduled agents. It supplies Ubuntu, Go, Buf, golangci-lint and Docker/runtime dependencies.

Agents must never ask the user to manually install/run Go, Docker, PostgreSQL, Redis, Kafka or tests when CI can perform validation.

### Cost-conscious selective CI

The repository uses selective validation because GitHub Actions minutes are limited.

Before human approval:
- use **selective CI only**;
- run checks based on the entire PR diff from base to current head, not merely the last commit;
- docs-only changes should avoid CI;
- Go/proto/module changes run relevant build/unit/lint/proto/architecture checks;
- runtime/integration checks run only for runtime-sensitive changes such as migrations, Docker/compose, integration tests and API-smoke scripts;
- `*_integration_test.go` is runtime-sensitive;
- integration scripts should start only dependencies they need (e.g. migration/repository tests start PostgreSQL only, not Kafka+Redis unnecessarily);
- avoid duplicate `push` + `pull_request` runs for the same SHA;
- do not rerun already-green checks on the exact same SHA without a reason.

### Full CI after human approval only

Full CI is a **merge gate**, not a pre-human-review gate.

Flow:

`implementation -> selective CI -> AI self-review/fix loop -> human review -> APPROVE -> full CI -> merge-ready`

A human APPROVE review should trigger full validation on the exact approved PR head SHA through the `pull_request_review` workflow behavior.

Do not create `[full-ci]`/no-op commits just to trigger full CI.

If full CI fails after approval:
1. fix the real issue;
2. run selective CI during iteration;
3. repeat AI self-review;
4. require human re-approval for the changed head;
5. run the approval-triggered full merge gate again.

Never auto-merge.

## 7. Runtime/integration testing philosophy

A green unit test is insufficient when an AC depends on real PostgreSQL semantics.

Prefer real PostgreSQL integration coverage for:
- migrations up/down;
- uniqueness/constraints;
- immutable snapshot triggers;
- `FOR UPDATE` / `SKIP LOCKED`;
- optimistic concurrency;
- transaction rollback/atomicity;
- concurrent workers/races;
- repository behavior;
- payment reconciliation leases/idempotency;
- Booking/Pricing/Payment schema integration.

Do not let tests create a private schema that bypasses the real migrations. Runtime tests should apply repository migrations, run against that schema, and verify rollback where applicable.

Generated protobuf should be generated before Go build/test in CI. Module manifests (`go.mod`, `go.sum`) must remain reproducible/consistent.

## 8. AI review gate

Every implementation PR must receive an AI senior-review pass before human review.

Review the complete PR diff plus relevant surrounding code/tests/design. Look for up to 10 genuinely important issues, prioritizing:
1. correctness/invariants;
2. concurrency/data races;
3. distributed recovery/idempotency;
4. security/data exposure;
5. transaction boundaries;
6. API/contract compatibility;
7. architecture violations;
8. resource leaks/performance;
9. missing/weak tests;
10. maintainability only when material.

Do not manufacture cosmetic issues to reach 10.

If important issues exist:
- request changes/comment as appropriate;
- fix them on the same task branch;
- run selective CI for changed scope;
- review again from scratch;
- repeat until `important_issues_remaining=0`.

GitHub does not allow the PR author to approve their own PR. When self-approval is rejected, leave a review/comment recording AI self-review pass and `important_issues_remaining=0`.

Only then mark the PR Ready for human review and move Jira to In Review.

## 9. Jira/GitHub task workflow

For each eligible task:
1. Read the full Jira issue: description, AC, labels, comments, seq and architecture links.
2. Resolve immediate predecessor status and predecessor PR/head.
3. Search GitHub for an existing PR for the Jira key; never duplicate implementation PRs.
4. Leave concise Jira run audit with candidate, predecessor, eligibility and reason.
5. If clear, transition to In Progress and add `codex-running`.
6. Read repo guidance, relevant code/tests and architecture sources.
7. Create/reuse `agent/<JIRA-KEY>-<slug>` from the correct stacked base.
8. Implement only requested scope; do not redesign unrelated architecture.
9. Add focused tests, including real runtime integration tests where semantics require them.
10. Push and use selective CI; fix failures.
11. Create/reuse Draft PR when relevant selective gates are green.
12. Perform AI self-review/fix/retest loop.
13. When selective CI is green and `important_issues_remaining=0`, remove `codex-running`, transition Jira to In Review, mark PR Ready for human review and record tested SHA/checks/review result in Jira.
14. After human approval, observe approval-triggered full CI and record merge-gate result. Do not merge automatically.

On blockers, use explicit audit reasons such as:
- `STACK_BASE_BLOCKED`
- `STACK_INCOMPLETE`
- `TEST_GATE_BLOCKED`
- `ARCHITECTURE_BLOCKED`
- permission/capability failure
- `NO WORK`

## 10. Scheduled worker

Automation name: `Jira GitHub Worker`.

Current intended schedule: hourly, exact schedule, timezone Asia/Ho_Chi_Minh. It is enabled at handoff time.

Primary candidate JQL concept:

`project = SCRUM AND status = "To Do" AND labels = "automation-ready" ORDER BY created ASC`

It also safely resumes `codex-running` work without creating duplicate branches/PRs.

Run audit comments are mandatory so a scheduler trigger can be distinguished from actual candidate discovery/claim/CI work.

The worker must obey the same stacked-PR, selective-CI, architecture precedence and AI-review rules as interactive work.

## 11. Current handoff state

Important current state at the end of the long planning/implementation session:
- SCRUM-25 (`seq-018`) was re-scoped to Payment recovery integration according to the newer architecture and is In Review with PR #74.
- SCRUM-36 (`seq-019`) is implemented and In Review with PR #75.
- PR #75 head at completion: `f0bb4f17e5df812080522423dd9a1a5ced7c8062`.
- PR #75 selective CI run used for pre-human gate: GitHub Actions run `31462468276`; required and runtime/integration validations were green.
- SCRUM-36 AI self-review required two iterations. The important issue fixed was making the cancellation snapshot mandatory at the atomic persistence boundary with booking-ID consistency checks. Final `important_issues_remaining=0`.
- `Jira GitHub Worker` was re-enabled after SCRUM-36 reached In Review.
- Next work should normally start from Jira `seq-020`, after re-reading live Jira/GitHub state rather than trusting this snapshot blindly.

## 12. Instructions for a new chat/session

Start by reading this file, then verify live state in Jira and GitHub.

Recommended opening request:

> Continue the hotel-booking project using `docs/PROJECT_HANDOFF.md` as the handoff/index. Re-read live Jira and GitHub state first. Follow seq-NNN ordering, stacked PRs, selective CI before human review, AI self-review until important_issues_remaining=0, and full CI only after human approval. Do not redesign approved architecture unless a current Jira/design source requires it.

The handoff is deliberately concise compared with conversation history. When implementation details matter, inspect the linked/current Jira issue, specialized architecture PRs, PR #20 architecture index, repository code and tests.