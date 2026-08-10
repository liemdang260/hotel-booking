//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/infrastructure/postgres"
	"github.com/liemdang260/hotel-booking/services/availability/internal/usecase"
)

const schema = `
DROP TABLE IF EXISTS availability_outbox_events, reservation_inventory, reservations, room_inventory CASCADE;

CREATE TABLE room_inventory (
    hotel_id UUID NOT NULL,
    room_type_id UUID NOT NULL,
    inventory_date DATE NOT NULL,
    total_quantity INTEGER NOT NULL CHECK (total_quantity >= 0),
    held_quantity INTEGER NOT NULL DEFAULT 0 CHECK (held_quantity >= 0),
    booked_quantity INTEGER NOT NULL DEFAULT 0 CHECK (booked_quantity >= 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (hotel_id, room_type_id, inventory_date),
    CHECK (held_quantity + booked_quantity <= total_quantity)
);
CREATE TABLE reservations (
    id UUID PRIMARY KEY,
    booking_id UUID NOT NULL UNIQUE,
    hotel_id UUID NOT NULL,
    room_type_id UUID NOT NULL,
    check_in DATE NOT NULL,
    check_out DATE NOT NULL CHECK (check_out > check_in),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    status VARCHAR(30) NOT NULL CHECK (status IN ('HELD','BOOKED','RELEASED','EXPIRED')),
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (status <> 'HELD' OR expires_at IS NOT NULL)
);
CREATE INDEX reservations_held_expiry_idx ON reservations (expires_at, id) WHERE status = 'HELD';
CREATE TABLE reservation_inventory (
    reservation_id UUID NOT NULL REFERENCES reservations(id),
    inventory_date DATE NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (reservation_id, inventory_date)
);
CREATE TABLE availability_outbox_events (
    id UUID PRIMARY KEY,
    aggregate_type VARCHAR(80) NOT NULL,
    aggregate_id UUID NOT NULL,
    aggregate_version BIGINT NOT NULL,
    event_type VARCHAR(120) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'PENDING',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    claimed_at TIMESTAMPTZ,
    claim_token UUID,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMPTZ
);`

const (
	hotelID = "00000000-0000-0000-0000-000000000001"
	roomTypeID = "00000000-0000-0000-0000-000000000002"
)

type fixedClock struct{ now time.Time }
func (c fixedClock) Now() time.Time { return c.now }

type sequenceIDs struct{ value atomic.Uint64 }
func newIDs(start uint64) *sequenceIDs {
	ids := &sequenceIDs{}
	ids.value.Store(start)
	return ids
}
func (g *sequenceIDs) next() string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", g.value.Add(1))
}
func (g *sequenceIDs) NewReservationID() domain.ReservationID { return domain.ReservationID(g.next()) }
func (g *sequenceIDs) NewEventID() domain.EventID { return domain.EventID(g.next()) }

type harness struct {
	db *sql.DB
	service *usecase.Service
	ids *sequenceIDs
	now time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dsn := os.Getenv("AVAILABILITY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set AVAILABILITY_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil { t.Fatal(err) }
	db.SetMaxOpenConns(120)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil { t.Fatal(err) }
	if _, err := db.ExecContext(context.Background(), schema); err != nil { t.Fatalf("reset schema: %v", err) }

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	ids := newIDs(10000)
	service := usecase.NewService(
		usecase.NewTransactionBoundary(postgres.NewTransactor(db)),
		ids,
		fixedClock{now:now},
	)
	return &harness{db:db, service:service, ids:ids, now:now}
}

func (h *harness) seedInventory(t *testing.T, checkIn time.Time, nights, total int) {
	t.Helper()
	for day := 0; day < nights; day++ {
		_, err := h.db.ExecContext(context.Background(), `
INSERT INTO room_inventory (hotel_id, room_type_id, inventory_date, total_quantity)
VALUES ($1, $2, $3, $4)`, hotelID, roomTypeID, checkIn.AddDate(0,0,day), total)
		if err != nil { t.Fatalf("seed inventory: %v", err) }
	}
}

func reserveInput(booking int, checkIn time.Time, nights int) usecase.ReserveInventoryInput {
	return usecase.ReserveInventoryInput{
		BookingID:domain.BookingID(fmt.Sprintf("00000000-0000-0000-0001-%012d", booking)),
		HotelID:domain.HotelID(hotelID), RoomTypeID:domain.RoomTypeID(roomTypeID),
		CheckIn:checkIn, CheckOut:checkIn.AddDate(0,0,nights),
		Quantity:1, HoldTTL:15*time.Minute,
	}
}

func inventoryCounters(t *testing.T, db *sql.DB, date time.Time) (held, booked int) {
	t.Helper()
	err := db.QueryRowContext(context.Background(), `
SELECT held_quantity, booked_quantity FROM room_inventory
WHERE hotel_id=$1 AND room_type_id=$2 AND inventory_date=$3`,
		hotelID, roomTypeID, date,
	).Scan(&held, &booked)
	if err != nil { t.Fatal(err) }
	return held, booked
}

func TestConcurrentReserveExactlyMatchesCapacity(t *testing.T) {
	h := newHarness(t)
	checkIn := time.Date(2026,10,1,0,0,0,0,time.UTC)
	h.seedInventory(t, checkIn, 1, 10)

	start := make(chan struct{})
	results := make(chan error, 100)
	var group sync.WaitGroup
	for i := 0; i < 100; i++ {
		group.Add(1)
		go func(request int) {
			defer group.Done()
			<-start
			_, err := h.service.ReserveInventory(context.Background(), reserveInput(1000+request, checkIn, 1))
			results <- err
		}(i)
	}
	close(start)
	group.Wait()
	close(results)

	successes := 0
	soldOut := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, usecase.ErrSoldOut):
			soldOut++
		default:
			t.Fatalf("unexpected reserve result: %v", err)
		}
	}
	if successes != 10 || soldOut != 90 {
		t.Fatalf("successes=%d sold_out=%d, want 10 and 90", successes, soldOut)
	}
	held, booked := inventoryCounters(t, h.db, checkIn)
	if held != 10 || booked != 0 {
		t.Fatalf("held=%d booked=%d, want 10 and 0", held, booked)
	}
}

func TestMissingNightRollsBackWholeReservation(t *testing.T) {
	h := newHarness(t)
	checkIn := time.Date(2026,10,2,0,0,0,0,time.UTC)
	h.seedInventory(t, checkIn, 1, 1)

	_, err := h.service.ReserveInventory(context.Background(), reserveInput(2000, checkIn, 2))
	if !errors.Is(err, usecase.ErrInventoryIncomplete) {
		t.Fatalf("expected incomplete inventory, got %v", err)
	}
	held, booked := inventoryCounters(t, h.db, checkIn)
	if held != 0 || booked != 0 { t.Fatalf("partial inventory mutation: held=%d booked=%d", held, booked) }
	var reservations int
	if err := h.db.QueryRow("SELECT count(*) FROM reservations").Scan(&reservations); err != nil { t.Fatal(err) }
	if reservations != 0 { t.Fatalf("reservations=%d, want 0", reservations) }
}

func TestDuplicateCommandsDoNotDoubleApplySideEffects(t *testing.T) {
	h := newHarness(t)
	checkIn := time.Date(2026,10,3,0,0,0,0,time.UTC)
	h.seedInventory(t, checkIn, 1, 2)

	input := reserveInput(3000, checkIn, 1)
	first, err := h.service.ReserveInventory(context.Background(), input)
	if err != nil { t.Fatal(err) }
	replayed, err := h.service.ReserveInventory(context.Background(), input)
	if err != nil { t.Fatal(err) }
	if first.ReservationID != replayed.ReservationID { t.Fatal("reserve replay returned different reservation") }
	if _, err := h.service.ConfirmReservation(context.Background(), first.ReservationID); err != nil { t.Fatal(err) }
	if _, err := h.service.ConfirmReservation(context.Background(), first.ReservationID); err != nil { t.Fatal(err) }

	releaseInput := reserveInput(3001, checkIn, 1)
	released, err := h.service.ReserveInventory(context.Background(), releaseInput)
	if err != nil { t.Fatal(err) }
	if _, err := h.service.ReleaseReservation(context.Background(), released.ReservationID); err != nil { t.Fatal(err) }
	if _, err := h.service.ReleaseReservation(context.Background(), released.ReservationID); err != nil { t.Fatal(err) }

	held, booked := inventoryCounters(t, h.db, checkIn)
	if held != 0 || booked != 1 { t.Fatalf("held=%d booked=%d, want 0 and 1", held, booked) }
	var reservations int
	if err := h.db.QueryRow("SELECT count(*) FROM reservations").Scan(&reservations); err != nil { t.Fatal(err) }
	if reservations != 2 { t.Fatalf("reservations=%d, want 2", reservations) }
}

func TestConfirmVersusExpirePreservesAllowedInvariant(t *testing.T) {
	h := newHarness(t)
	checkIn := time.Date(2026,10,4,0,0,0,0,time.UTC)
	h.seedInventory(t, checkIn, 1, 1)
	reserved, err := h.service.ReserveInventory(context.Background(), reserveInput(4000, checkIn, 1))
	if err != nil { t.Fatal(err) }

	expirer := usecase.NewExpireReservations(
		postgres.NewExpirationTransactor(h.db),
		h.ids,
		fixedClock{now:h.now.Add(20*time.Minute)},
		10,
	)
	start := make(chan struct{})
	confirmResult := make(chan error, 1)
	expireResult := make(chan error, 1)
	go func() {
		<-start
		_, err := h.service.ConfirmReservation(context.Background(), reserved.ReservationID)
		confirmResult <- err
	}()
	go func() {
		<-start
		_, err := expirer.ExecuteBatch(context.Background(), 1)
		expireResult <- err
	}()
	close(start)
	confirmErr := <-confirmResult
	expireErr := <-expireResult
	if confirmErr != nil && !errors.Is(confirmErr, usecase.ErrInvalidTransition) {
		t.Fatalf("unexpected confirm error: %v", confirmErr)
	}
	if expireErr != nil { t.Fatalf("expiration error: %v", expireErr) }

	var statusValue string
	if err := h.db.QueryRow("SELECT status FROM reservations WHERE id=$1", reserved.ReservationID).Scan(&statusValue); err != nil {
		t.Fatal(err)
	}
	held, booked := inventoryCounters(t, h.db, checkIn)
	var expiredEvents int
	if err := h.db.QueryRow("SELECT count(*) FROM availability_outbox_events WHERE event_type='ReservationExpired'").Scan(&expiredEvents); err != nil {
		t.Fatal(err)
	}
	switch domain.ReservationStatus(statusValue) {
	case domain.ReservationBooked:
		if held != 0 || booked != 1 || expiredEvents != 0 {
			t.Fatalf("BOOKED outcome invariant failed: held=%d booked=%d events=%d", held, booked, expiredEvents)
		}
	case domain.ReservationExpired:
		if held != 0 || booked != 0 || expiredEvents != 1 {
			t.Fatalf("EXPIRED outcome invariant failed: held=%d booked=%d events=%d", held, booked, expiredEvents)
		}
	default:
		t.Fatalf("unexpected final status %s", statusValue)
	}
}
