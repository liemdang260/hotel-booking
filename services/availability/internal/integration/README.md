# Availability integration tests

These tests exercise the real PostgreSQL locking and transaction behavior for:

- 100 concurrent reserve requests against capacity 10;
- all-or-nothing multi-night reservation;
- replay-safe reserve, confirm, and release commands;
- the confirm-versus-expire race and its allowed terminal invariants.

Use a disposable PostgreSQL database. The suite drops and recreates the Availability tables.

Run after the prerequisite Availability implementation PRs have been integrated:

```sh
export AVAILABILITY_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/availability_test?sslmode=disable'
go test -tags=integration ./services/availability/internal/integration -count=1
```

The test package is excluded from normal unit-test runs by the integration build tag.
