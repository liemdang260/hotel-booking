//go:build integration

package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/availability/internal/usecase"
)

func TestSoldOutNightRollsBackWholeReservation(t *testing.T) {
	h := newHarness(t)
	checkIn := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	h.seedInventory(t, checkIn, 2, 1)

	if _, err := h.db.ExecContext(context.Background(), `
UPDATE room_inventory
SET booked_quantity = 1
WHERE hotel_id = $1 AND room_type_id = $2 AND inventory_date = $3`,
		hotelID, roomTypeID, checkIn.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("mark second night sold out: %v", err)
	}

	_, err := h.service.ReserveInventory(context.Background(), reserveInput(5000, checkIn, 2))
	if !errors.Is(err, usecase.ErrSoldOut) {
		t.Fatalf("expected sold out, got %v", err)
	}

	firstHeld, firstBooked := inventoryCounters(t, h.db, checkIn)
	if firstHeld != 0 || firstBooked != 0 {
		t.Fatalf("first night partially mutated: held=%d booked=%d", firstHeld, firstBooked)
	}
	secondHeld, secondBooked := inventoryCounters(t, h.db, checkIn.AddDate(0, 0, 1))
	if secondHeld != 0 || secondBooked != 1 {
		t.Fatalf("sold-out night changed unexpectedly: held=%d booked=%d", secondHeld, secondBooked)
	}

	var reservations int
	if err := h.db.QueryRow("SELECT count(*) FROM reservations").Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if reservations != 0 {
		t.Fatalf("reservations=%d, want 0", reservations)
	}
}
