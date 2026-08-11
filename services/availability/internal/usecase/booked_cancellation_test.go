package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
)

func TestCancelBookedReservationReplayReturnsInventoryOnce(t *testing.T) {
	service, inventory, reservations, input := fixture(t)
	reserved, err := service.ReserveInventory(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmReservation(context.Background(), reserved.ReservationID); err != nil {
		t.Fatal(err)
	}

	inventory.saves = 0
	reservations.saves = 0
	first, err := service.CancelBookedReservation(context.Background(), reserved.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.CancelBookedReservation(context.Background(), reserved.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != domain.ReservationCancelled || replayed.Status != domain.ReservationCancelled {
		t.Fatalf("statuses=(%s,%s), want CANCELLED", first.Status, replayed.Status)
	}
	if inventory.saves != 2 || reservations.saves != 1 {
		t.Fatalf("cancel replay mutated state: inventory saves=%d reservation saves=%d", inventory.saves, reservations.saves)
	}
	for _, item := range inventory.items {
		if item.HeldQuantity != 0 || item.BookedQuantity != 0 {
			t.Fatalf("unexpected counters: held=%d booked=%d", item.HeldQuantity, item.BookedQuantity)
		}
	}
}

func TestCancelBookedReservationRejectsHeldReservation(t *testing.T) {
	service, inventory, reservations, input := fixture(t)
	reserved, err := service.ReserveInventory(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	inventory.saves = 0
	reservations.saves = 0

	_, err = service.CancelBookedReservation(context.Background(), reserved.ReservationID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
	if inventory.saves != 0 || reservations.saves != 0 {
		t.Fatalf("invalid cancellation mutated state: inventory saves=%d reservation saves=%d", inventory.saves, reservations.saves)
	}
}
