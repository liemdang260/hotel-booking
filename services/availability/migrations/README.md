# Availability migrations

The files in this directory are plain PostgreSQL migrations. Run them with a
migration runner that applies each file as a single unit, or directly with
`psql`.

## Verify the initial migration

Use a disposable Availability database:

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -f services/availability/migrations/000001_create_availability_schema.up.sql

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -f services/availability/migrations/000001_create_availability_schema_test.sql

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -f services/availability/migrations/000001_create_availability_schema.down.sql
```

The verification migration runs inside a transaction and rolls back its fixture
data. It checks the expected tables and PostgreSQL date/time types, verifies
that the HELD-expiry index is partial, and exercises capacity, booking
idempotency, reservation-state, reservation-inventory, and outbox constraints.

The Availability database owns these tables. The opaque Catalog and Booking IDs
therefore have no cross-service foreign keys.
