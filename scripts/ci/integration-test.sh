#!/bin/sh
set -eu

COMPOSE_FILE="deployments/docker-compose.yml"
ENV_FILE="deployments/.env"

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "Missing $COMPOSE_FILE" >&2
  exit 1
fi

if [ ! -f "$ENV_FILE" ]; then
  if [ -f deployments/.env.example ]; then
    cp deployments/.env.example "$ENV_FILE"
  else
    echo "Missing $ENV_FILE and deployments/.env.example" >&2
    exit 1
  fi
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

POSTGRES_USER="${POSTGRES_USER:-hotel}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-hotel}"
POSTGRES_DB="${POSTGRES_DB:-hotel_booking}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"

compose() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

cleanup() {
  compose down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

psql_in_postgres() {
  compose exec -T postgres \
    psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 "$@"
}

AVAILABILITY_UP="services/availability/migrations/000001_create_availability_schema.up.sql"
AVAILABILITY_VERIFY="services/availability/migrations/000001_create_availability_schema_test.sql"
AVAILABILITY_DOWN="services/availability/migrations/000001_create_availability_schema.down.sql"
BOOKING_UP="services/booking/migrations/000001_create_booking_schema.up.sql"
BOOKING_DOWN="services/booking/migrations/000001_create_booking_schema.down.sql"

for file in "$AVAILABILITY_UP" "$AVAILABILITY_VERIFY" "$AVAILABILITY_DOWN"; do
  if [ ! -f "$file" ]; then
    echo "Missing migration test input: $file" >&2
    exit 1
  fi
done

if [ -f "$BOOKING_UP" ] || [ -f "$BOOKING_DOWN" ]; then
  for file in "$BOOKING_UP" "$BOOKING_DOWN"; do
    if [ ! -f "$file" ]; then
      echo "Incomplete Booking migration pair: missing $file" >&2
      exit 1
    fi
  done
fi

echo "Starting PostgreSQL only for integration validation"
compose up -d --wait postgres
compose ps postgres

export AVAILABILITY_TEST_DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"
export BOOKING_TEST_DATABASE_URL="$AVAILABILITY_TEST_DATABASE_URL"

echo "Applying Availability migration"
psql_in_postgres < "$AVAILABILITY_UP"

echo "Executing Availability schema verification"
psql_in_postgres < "$AVAILABILITY_VERIFY"

echo "Running Availability repository integration tests"
go test -count=1 -tags=integration ./services/availability/internal/infrastructure/postgres -run '^TestIntegration'

if [ -d services/availability/internal/integration ]; then
  echo "Running Availability concurrency and idempotency integration tests"
  go test -count=1 -tags=integration ./services/availability/internal/integration
fi

echo "Rolling back Availability migration"
psql_in_postgres < "$AVAILABILITY_DOWN"

echo "Verifying rollback removed Availability tables"
availability_remaining="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relname IN ('room_inventory','reservations','reservation_inventory','availability_outbox_events');")"
if [ "$availability_remaining" != "0" ]; then
  echo "Availability rollback left $availability_remaining owned tables behind" >&2
  exit 1
fi

echo "Availability integration validation passed."

if [ -f "$BOOKING_UP" ]; then
  echo "Applying Booking migration"
  psql_in_postgres < "$BOOKING_UP"

  if [ -d services/booking/internal/infrastructure/postgres ]; then
    echo "Running Booking repository and transaction integration tests"
    go test -count=1 -tags=integration ./services/booking/internal/infrastructure/postgres -run '^TestIntegration'
  fi

  echo "Rolling back Booking migration"
  psql_in_postgres < "$BOOKING_DOWN"

  echo "Verifying rollback removed Booking tables"
  booking_remaining="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relname IN ('bookings','booking_price_snapshots','booking_sagas','booking_idempotency','booking_outbox_events');")"
  if [ "$booking_remaining" != "0" ]; then
    echo "Booking rollback left $booking_remaining owned tables behind" >&2
    exit 1
  fi

  echo "Booking migration and repository integration validation passed."
fi
