//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/usecase"
)

const (
	integrationReservationID domain.ReservationID = "00000000-0000-0000-0000-000000000201"
	integrationBookingID     domain.BookingID     = "00000000-0000-0000-0000-000000000301"
)

type integrationClock struct {
	now time.Time
}

func (c integrationClock) Now() time.Time { return c.now }

type integrationEventIDs struct {
	mu   sync.Mutex
	next int
}

func (g *integrationEventIDs) NewEventID() domain.EventID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next++
	return domain.EventID(fmt.Sprintf("00000000-0000-0000-0000-%012d", 400+g.next))
}

type invalidIntegrationEventIDs struct{}

func (invalidIntegrationEventIDs) NewEventID() domain.EventID { return "not-a-uuid" }

func resetExpirationFixture(t *testing.T, now time.Time) {
	t.Helper()
	db := openIntegrationDB(t)
	resetInventoryFixture(t, db)

	_, err := db.Exec(`
UPDATE room_inventory
SET held_quantity = 2
WHERE hotel_id = '00000000-0000-0000-0000-000000000101'
  AND room_type_id = '00000000-0000-0000-0000-000000000102';

INSERT INTO reservations (
    id, booking_id, hotel_id, room_type_id,
    check_in, check_out, quantity, status, expires_at,
    created_at, updated_at
) VALUES (
    '00000000-0000-0000-0000-000000000201',
    '00000000-0000-0000-0000-000000000301',
    '00000000-0000-0000-0000-000000000101',
    '00000000-0000-0000-0000-000000000102',
    DATE '2026-09-01', DATE '2026-09-03', 2, 'HELD', $1,
    $2, $2
);

INSERT INTO reservation_inventory (reservation_id, inventory_date, quantity) VALUES
('00000000-0000-0000-0000-000000000201', DATE '2026-09-01', 2),
('00000000-0000-0000-0000-000000000201', DATE '2026-09-02', 2);`, now.Add(-time.Minute), now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("reset expiration fixture: %v", err)
	}
}

func assertExpirationState(
	t *testing.T,
	status domain.ReservationStatus,
	heldQuantity int,
	bookedQuantity int,
	outboxCount int,
) {
	t.Helper()
	db := openIntegrationDB(t)

	var actualStatus domain.ReservationStatus
	if err := db.QueryRow(`SELECT status FROM reservations WHERE id = $1`, integrationReservationID).Scan(&actualStatus); err != nil {
		t.Fatalf("read reservation status: %v", err)
	}
	if actualStatus != status {
		t.Fatalf("reservation status=%s, want %s", actualStatus, status)
	}

	rows, err := db.Query(`
SELECT held_quantity, booked_quantity
FROM room_inventory
WHERE hotel_id = '00000000-0000-0000-0000-000000000101'
  AND room_type_id = '00000000-0000-0000-0000-000000000102'
ORDER BY inventory_date`)
	if err != nil {
		t.Fatalf("read inventory state: %v", err)
	}
	defer rows.Close()

	rowCount := 0
	for rows.Next() {
		rowCount++
		var held, booked int
		if err := rows.Scan(&held, &booked); err != nil {
			t.Fatalf("scan inventory state: %v", err)
		}
		if held != heldQuantity || booked != bookedQuantity {
			t.Fatalf("inventory row %d held=%d booked=%d, want held=%d booked=%d", rowCount, held, booked, heldQuantity, bookedQuantity)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate inventory state: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("inventory row count=%d, want 2", rowCount)
	}

	var actualOutboxCount int
	if err := db.QueryRow(`SELECT count(*) FROM availability_outbox_events WHERE aggregate_id = $1`, integrationReservationID).Scan(&actualOutboxCount); err != nil {
		t.Fatalf("count expiration outbox events: %v", err)
	}
	if actualOutboxCount != outboxCount {
		t.Fatalf("outbox count=%d, want %d", actualOutboxCount, outboxCount)
	}
}

func TestIntegrationExpirationWorkersDoNotExpireReservationTwice(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	resetExpirationFixture(t, now)
	db := openIntegrationDB(t)

	expire := usecase.NewExpireReservations(
		NewExpirationTransactor(db),
		&integrationEventIDs{},
		integrationClock{now: now},
		10,
	)

	start := make(chan struct{})
	counts := make(chan int, 2)
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			count, err := expire.ExecuteBatch(context.Background(), 1)
			counts <- count
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(counts)
	close(errs)

	total := 0
	for count := range counts {
		total += count
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("expiration worker failed: %v", err)
		}
	}
	if total != 1 {
		t.Fatalf("total expired reservations=%d, want 1", total)
	}
	assertExpirationState(t, domain.ReservationExpired, 0, 0, 1)
}

func TestIntegrationExpirationOutboxFailureRollsBackState(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	resetExpirationFixture(t, now)
	db := openIntegrationDB(t)

	expire := usecase.NewExpireReservations(
		NewExpirationTransactor(db),
		invalidIntegrationEventIDs{},
		integrationClock{now: now},
		10,
	)

	count, err := expire.ExecuteBatch(context.Background(), 1)
	if err == nil {
		t.Fatal("expected invalid outbox id to fail expiration transaction")
	}
	if count != 0 {
		t.Fatalf("expired count=%d after rollback, want 0", count)
	}
	assertExpirationState(t, domain.ReservationHeld, 2, 0, 0)
}

func TestIntegrationConfirmVsExpireRaceIsSerialized(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	resetExpirationFixture(t, now)
	db := openIntegrationDB(t)

	expire := usecase.NewExpireReservations(
		NewExpirationTransactor(db),
		&integrationEventIDs{},
		integrationClock{now: now},
		10,
	)
	availability := usecase.NewService(
		usecase.NewTransactionBoundary(NewTransactor(db)),
		nil,
		integrationClock{now: now},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})

	var confirmErr error
	var expireErr error
	var expireCount int
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		_, confirmErr = availability.ConfirmReservation(ctx, integrationReservationID)
	}()
	go func() {
		defer group.Done()
		<-start
		expireCount, expireErr = expire.ExecuteBatch(ctx, 1)
	}()
	close(start)
	group.Wait()

	if expireErr != nil {
		t.Fatalf("expiration side of race failed: %v", expireErr)
	}

	var status domain.ReservationStatus
	if err := db.QueryRow(`SELECT status FROM reservations WHERE id = $1`, integrationReservationID).Scan(&status); err != nil {
		t.Fatalf("read terminal reservation state: %v", err)
	}

	switch status {
	case domain.ReservationBooked:
		if confirmErr != nil {
			t.Fatalf("confirm won but returned error: %v", confirmErr)
		}
		if expireCount != 0 {
			t.Fatalf("confirm won but expiration also mutated %d reservation(s)", expireCount)
		}
		assertExpirationState(t, domain.ReservationBooked, 0, 2, 0)
	case domain.ReservationExpired:
		if expireCount != 1 {
			t.Fatalf("expiration won but expired count=%d, want 1", expireCount)
		}
		if !errors.Is(confirmErr, usecase.ErrInvalidTransition) {
			t.Fatalf("expiration won; confirm error=%v, want ErrInvalidTransition", confirmErr)
		}
		assertExpirationState(t, domain.ReservationExpired, 0, 0, 1)
	default:
		t.Fatalf("race left unexpected reservation status %s", status)
	}
}
