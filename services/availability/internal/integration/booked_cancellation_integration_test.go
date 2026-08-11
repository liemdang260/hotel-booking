//go:build integration

package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
)

func TestBookedCancellationConcurrentReplayReturnsInventoryExactlyOnce(t *testing.T) {
	h := newHarness(t)
	checkIn := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	h.seedInventory(t, checkIn, 2, 2)

	reserved, err := h.service.ReserveInventory(context.Background(), reserveInput(5000, checkIn, 2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.ConfirmReservation(context.Background(), reserved.ReservationID); err != nil {
		t.Fatal(err)
	}

	const callers = 20
	start := make(chan struct{})
	results := make(chan error, callers)
	var group sync.WaitGroup
	for i := 0; i < callers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := h.service.CancelBookedReservation(context.Background(), reserved.ReservationID)
			results <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent cancellation failed: %v", err)
		}
	}

	var status string
	if err := h.db.QueryRow("SELECT status FROM reservations WHERE id=$1", reserved.ReservationID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if domain.ReservationStatus(status) != domain.ReservationCancelled {
		t.Fatalf("status=%s, want CANCELLED", status)
	}
	for day := 0; day < 2; day++ {
		held, booked := inventoryCounters(t, h.db, checkIn.AddDate(0, 0, day))
		if held != 0 || booked != 0 {
			t.Fatalf("day=%d held=%d booked=%d, want both zero", day, held, booked)
		}
	}
}
