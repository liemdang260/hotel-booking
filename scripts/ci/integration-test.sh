#!/bin/sh
set -eu

COMPOSE_FILE="deployments/docker-compose.yml"
ENV_FILE="deployments/.env"

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "Missing $COMPOSE_FILE" >&2
  exit 1
fi

if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi

POSTGRES_USER="${POSTGRES_USER:-hotel}"
POSTGRES_DB="${POSTGRES_DB:-hotel_booking}"

psql_in_postgres() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" exec -T postgres \
    psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 "$@"
}

UP="services/availability/migrations/000001_create_availability_schema.up.sql"
VERIFY="services/availability/migrations/000001_create_availability_schema_test.sql"
DOWN="services/availability/migrations/000001_create_availability_schema.down.sql"

for file in "$UP" "$VERIFY" "$DOWN"; do
  if [ ! -f "$file" ]; then
    echo "Missing migration test input: $file" >&2
    exit 1
  fi
done

echo "Applying Availability migration"
psql_in_postgres < "$UP"

echo "Executing Availability schema verification"
psql_in_postgres < "$VERIFY"

echo "Rolling back Availability migration"
psql_in_postgres < "$DOWN"

echo "Verifying rollback removed Availability tables"
remaining="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relname IN ('room_inventory','reservations','reservation_inventory','availability_outbox_events');")"
if [ "$remaining" != "0" ]; then
  echo "Availability rollback left $remaining owned tables behind" >&2
  exit 1
fi

echo "Availability migration apply/verify/rollback passed."
