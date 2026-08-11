#!/bin/sh
set -eu

COMPOSE_FILE="deployments/docker-compose.yml"
ENV_FILE="deployments/.env"

if [ ! -f "$COMPOSE_FILE" ]; then echo "Missing $COMPOSE_FILE" >&2; exit 1; fi
if [ ! -f "$ENV_FILE" ]; then
  if [ -f deployments/.env.example ]; then cp deployments/.env.example "$ENV_FILE"; else echo "Missing $ENV_FILE and deployments/.env.example" >&2; exit 1; fi
fi
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
POSTGRES_USER="${POSTGRES_USER:-hotel}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-hotel}"
POSTGRES_DB="${POSTGRES_DB:-hotel_booking}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
compose(){ docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"; }
cleanup(){ compose down -v --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM
psql_in_postgres(){ compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 "$@"; }

AUTH_UP="services/auth/migrations/000001_create_auth_schema.up.sql"
AUTH_DOWN="services/auth/migrations/000001_create_auth_schema.down.sql"
CATALOG_UP="services/catalog/migrations/000001_create_catalog_schema.up.sql"
CATALOG_DOWN="services/catalog/migrations/000001_create_catalog_schema.down.sql"
AVAILABILITY_UP="services/availability/migrations/000001_create_availability_schema.up.sql"
AVAILABILITY_VERIFY="services/availability/migrations/000001_create_availability_schema_test.sql"
AVAILABILITY_DOWN="services/availability/migrations/000001_create_availability_schema.down.sql"
AVAILABILITY_CANCELLATION_UP="services/availability/migrations/000002_add_booked_reservation_cancellation.up.sql"
AVAILABILITY_CANCELLATION_DOWN="services/availability/migrations/000002_add_booked_reservation_cancellation.down.sql"
AVAILABILITY_OUTBOX_UP="services/availability/migrations/000003_add_outbox_publisher_leases.up.sql"
AVAILABILITY_OUTBOX_DOWN="services/availability/migrations/000003_add_outbox_publisher_leases.down.sql"
BOOKING_UP="services/booking/migrations/000001_create_booking_schema.up.sql"
BOOKING_DOWN="services/booking/migrations/000001_create_booking_schema.down.sql"
BOOKING_POLICY_UP="services/booking/migrations/000002_create_cancellation_policy_snapshots.up.sql"
BOOKING_POLICY_DOWN="services/booking/migrations/000002_create_cancellation_policy_snapshots.down.sql"
BOOKING_CANCELLATION_UP="services/booking/migrations/000003_create_booking_cancellations.up.sql"
BOOKING_CANCELLATION_DOWN="services/booking/migrations/000003_create_booking_cancellations.down.sql"
BOOKING_OUTBOX_UP="services/booking/migrations/000004_add_outbox_publisher_leases.up.sql"
BOOKING_OUTBOX_DOWN="services/booking/migrations/000004_add_outbox_publisher_leases.down.sql"
PRICING_UP="services/pricing/migrations/000001_create_quotes.up.sql"
PRICING_DOWN="services/pricing/migrations/000001_create_quotes.down.sql"
PRICING_POLICY_UP="services/pricing/migrations/000002_add_cancellation_policy.up.sql"
PRICING_POLICY_DOWN="services/pricing/migrations/000002_add_cancellation_policy.down.sql"
PAYMENT_UP="services/payment/migrations/000001_create_payments.up.sql"
PAYMENT_DOWN="services/payment/migrations/000001_create_payments.down.sql"
PAYMENT_RECON_UP="services/payment/migrations/000002_create_payment_reconciliations.up.sql"
PAYMENT_RECON_DOWN="services/payment/migrations/000002_create_payment_reconciliations.down.sql"
PAYMENT_REFUND_UP="services/payment/migrations/000003_create_refunds.up.sql"
PAYMENT_REFUND_DOWN="services/payment/migrations/000003_create_refunds.down.sql"

for file in "$AVAILABILITY_UP" "$AVAILABILITY_VERIFY" "$AVAILABILITY_DOWN"; do [ -f "$file" ] || { echo "Missing migration test input: $file" >&2; exit 1; }; done
for pair in "Auth:$AUTH_UP:$AUTH_DOWN" "Catalog:$CATALOG_UP:$CATALOG_DOWN" "Availability booked cancellation:$AVAILABILITY_CANCELLATION_UP:$AVAILABILITY_CANCELLATION_DOWN" "Availability outbox publisher:$AVAILABILITY_OUTBOX_UP:$AVAILABILITY_OUTBOX_DOWN" "Booking:$BOOKING_UP:$BOOKING_DOWN" "Booking cancellation policy:$BOOKING_POLICY_UP:$BOOKING_POLICY_DOWN" "Booking cancellation workflow:$BOOKING_CANCELLATION_UP:$BOOKING_CANCELLATION_DOWN" "Booking outbox publisher:$BOOKING_OUTBOX_UP:$BOOKING_OUTBOX_DOWN" "Pricing:$PRICING_UP:$PRICING_DOWN" "Pricing cancellation policy:$PRICING_POLICY_UP:$PRICING_POLICY_DOWN" "Payment:$PAYMENT_UP:$PAYMENT_DOWN" "Payment reconciliation:$PAYMENT_RECON_UP:$PAYMENT_RECON_DOWN" "Payment refunds:$PAYMENT_REFUND_UP:$PAYMENT_REFUND_DOWN"; do
  name="${pair%%:*}"; rest="${pair#*:}"; up="${rest%%:*}"; down="${rest#*:}"
  if [ -f "$up" ] || [ -f "$down" ]; then [ -f "$up" ] && [ -f "$down" ] || { echo "Incomplete $name migration pair" >&2; exit 1; }; fi
done
[ ! -f "$BOOKING_POLICY_UP" ] || [ -f "$BOOKING_UP" ] || { echo "Booking policy migration requires base Booking migration" >&2; exit 1; }
[ ! -f "$BOOKING_CANCELLATION_UP" ] || [ -f "$BOOKING_POLICY_UP" ] || { echo "Booking cancellation workflow requires policy snapshot migration" >&2; exit 1; }
[ ! -f "$PRICING_POLICY_UP" ] || [ -f "$PRICING_UP" ] || { echo "Pricing policy migration requires base Pricing migration" >&2; exit 1; }
[ ! -f "$PAYMENT_RECON_UP" ] || [ -f "$PAYMENT_UP" ] || { echo "Payment reconciliation migration requires base Payment migration" >&2; exit 1; }
[ ! -f "$PAYMENT_REFUND_UP" ] || [ -f "$PAYMENT_UP" ] || { echo "Payment refund migration requires base Payment migration" >&2; exit 1; }

echo "Starting PostgreSQL only for integration validation"
compose up -d --wait postgres
compose ps postgres
export AUTH_TEST_DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"
export CATALOG_TEST_DATABASE_URL="$AUTH_TEST_DATABASE_URL"
export AVAILABILITY_TEST_DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"
export BOOKING_TEST_DATABASE_URL="$AVAILABILITY_TEST_DATABASE_URL"
export PRICING_TEST_DATABASE_URL="$AVAILABILITY_TEST_DATABASE_URL"
export PAYMENT_TEST_DATABASE_URL="$AVAILABILITY_TEST_DATABASE_URL"

echo "Applying Availability migration"; psql_in_postgres < "$AVAILABILITY_UP"
echo "Applying Availability booked-cancellation migration"; psql_in_postgres < "$AVAILABILITY_CANCELLATION_UP"
echo "Applying Availability outbox publisher migration"; psql_in_postgres < "$AVAILABILITY_OUTBOX_UP"
echo "Executing Availability schema verification"; psql_in_postgres < "$AVAILABILITY_VERIFY"
echo "Running Availability repository integration tests"; go test -count=1 -tags=integration ./services/availability/internal/infrastructure/postgres -run '^TestIntegration'
if [ -d services/availability/internal/integration ]; then echo "Running Availability concurrency and idempotency integration tests"; go test -count=1 -tags=integration ./services/availability/internal/integration; fi
echo "Clearing Availability test data before migration rollback"; psql_in_postgres -c "TRUNCATE availability_outbox_events, reservation_inventory, reservations, room_inventory CASCADE"
echo "Rolling back Availability outbox publisher migration"; psql_in_postgres < "$AVAILABILITY_OUTBOX_DOWN"
echo "Rolling back Availability booked-cancellation migration"; psql_in_postgres < "$AVAILABILITY_CANCELLATION_DOWN"
echo "Rolling back Availability migration"; psql_in_postgres < "$AVAILABILITY_DOWN"
availability_remaining="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname IN ('room_inventory','reservations','reservation_inventory','availability_outbox_events');")"
[ "$availability_remaining" = "0" ] || { echo "Availability rollback left $availability_remaining tables" >&2; exit 1; }

echo "Availability integration validation passed."

if [ -f "$AUTH_UP" ]; then
  echo "Applying Auth migration"; psql_in_postgres < "$AUTH_UP"
  if [ -d services/auth/internal/infrastructure/postgres ]; then echo "Running Auth repository integration tests"; go test -count=1 -tags=integration ./services/auth/internal/infrastructure/postgres -run '^TestIntegration'; fi
  echo "Rolling back Auth migration"; psql_in_postgres < "$AUTH_DOWN"
  auth_remaining="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname IN ('auth_users','auth_refresh_tokens');")"
  [ "$auth_remaining" = "0" ] || { echo "Auth rollback left $auth_remaining tables" >&2; exit 1; }
  echo "Auth migration validation passed."
fi

if [ -f "$CATALOG_UP" ]; then
  echo "Applying Catalog migration"; psql_in_postgres < "$CATALOG_UP"
  if [ -d services/catalog/internal/infrastructure/postgres ]; then echo "Running Catalog repository integration tests"; go test -count=1 -tags=integration ./services/catalog/internal/infrastructure/postgres -run '^TestIntegration'; fi
  echo "Rolling back Catalog migration"; psql_in_postgres < "$CATALOG_DOWN"
  catalog_remaining="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname IN ('catalog_hotels','catalog_room_types');")"
  [ "$catalog_remaining" = "0" ] || { echo "Catalog rollback left $catalog_remaining tables" >&2; exit 1; }
  echo "Catalog migration validation passed."
fi

if [ -f "$BOOKING_UP" ]; then
  echo "Applying Booking migration"; psql_in_postgres < "$BOOKING_UP"
  if [ -f "$BOOKING_POLICY_UP" ]; then echo "Applying Booking cancellation policy migration"; psql_in_postgres < "$BOOKING_POLICY_UP"; fi
  if [ -f "$BOOKING_CANCELLATION_UP" ]; then echo "Applying Booking cancellation workflow migration"; psql_in_postgres < "$BOOKING_CANCELLATION_UP"; fi
  echo "Applying Booking outbox publisher migration"; psql_in_postgres < "$BOOKING_OUTBOX_UP"
  echo "Running Booking repository and transaction integration tests"; go test -count=1 -tags=integration ./services/booking/internal/infrastructure/postgres -run '^TestIntegration'
  echo "Rolling back Booking outbox publisher migration"; psql_in_postgres < "$BOOKING_OUTBOX_DOWN"
  if [ -f "$BOOKING_CANCELLATION_DOWN" ]; then echo "Rolling back Booking cancellation workflow migration"; psql_in_postgres < "$BOOKING_CANCELLATION_DOWN"; fi
  if [ -f "$BOOKING_POLICY_DOWN" ]; then echo "Rolling back Booking cancellation policy migration"; psql_in_postgres < "$BOOKING_POLICY_DOWN"; fi
  echo "Rolling back Booking migration"; psql_in_postgres < "$BOOKING_DOWN"
  booking_remaining="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname IN ('bookings','booking_price_snapshots','booking_cancellation_policies','booking_cancellations','booking_sagas','booking_idempotency','booking_outbox_events');")"
  booking_functions="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.proname='reject_booking_cancellation_policy_update';")"
  [ "$booking_remaining" = "0" ] && [ "$booking_functions" = "0" ] || { echo "Booking rollback left tables=$booking_remaining functions=$booking_functions" >&2; exit 1; }
  echo "Booking migration and repository integration validation passed."
fi

if [ -f "$PRICING_UP" ]; then
  echo "Applying Pricing migration"; psql_in_postgres < "$PRICING_UP"
  if [ -f "$PRICING_POLICY_UP" ]; then echo "Applying Pricing cancellation policy migration"; psql_in_postgres < "$PRICING_POLICY_UP"; fi
  echo "Running Pricing repository integration tests"; go test -count=1 -tags=integration ./services/pricing/internal/infrastructure/postgres -run '^TestIntegration'
  if [ -f "$PRICING_POLICY_DOWN" ]; then echo "Rolling back Pricing cancellation policy migration"; psql_in_postgres < "$PRICING_POLICY_DOWN"; fi
  echo "Rolling back Pricing migration"; psql_in_postgres < "$PRICING_DOWN"
  pricing_tables="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname='quotes';")"
  pricing_functions="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.proname='reject_quote_update';")"
  [ "$pricing_tables" = "0" ] && [ "$pricing_functions" = "0" ] || { echo "Pricing rollback left tables=$pricing_tables functions=$pricing_functions" >&2; exit 1; }
  echo "Pricing migration and repository integration validation passed."
fi

if [ -f "$PAYMENT_UP" ]; then
  echo "Applying Payment migration"; psql_in_postgres < "$PAYMENT_UP"
  if [ -f "$PAYMENT_RECON_UP" ]; then echo "Applying Payment reconciliation migration"; psql_in_postgres < "$PAYMENT_RECON_UP"; fi
  if [ -f "$PAYMENT_REFUND_UP" ]; then echo "Applying Payment refund migration"; psql_in_postgres < "$PAYMENT_REFUND_UP"; fi
  echo "Running Payment repository integration tests"; go test -count=1 -tags=integration ./services/payment/internal/infrastructure/postgres -run '^TestIntegration'
  if [ -f "$PAYMENT_REFUND_DOWN" ]; then echo "Rolling back Payment refund migration"; psql_in_postgres < "$PAYMENT_REFUND_DOWN"; fi
  if [ -f "$PAYMENT_RECON_DOWN" ]; then echo "Rolling back Payment reconciliation migration"; psql_in_postgres < "$PAYMENT_RECON_DOWN"; fi
  echo "Rolling back Payment migration"; psql_in_postgres < "$PAYMENT_DOWN"
  payment_remaining="$(psql_in_postgres -Atqc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname IN ('payments','payment_attempts','payment_reconciliations','refunds','refund_attempts');")"
  [ "$payment_remaining" = "0" ] || { echo "Payment rollback left $payment_remaining tables" >&2; exit 1; }
  echo "Payment migration and repository integration validation passed."
fi
