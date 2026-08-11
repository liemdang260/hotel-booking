package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/domain/repository"
)

type fixedClock struct{ now time.Time }
func (c fixedClock) Now() time.Time { return c.now }

type fixedIDs struct{ id domain.ReservationID }
func (g fixedIDs) NewReservationID() domain.ReservationID { return g.id }

type fakeTransactions struct{ repos repository.Repositories }
func (f fakeTransactions) WithinTransaction(ctx context.Context, work repository.TransactionWork) error {
	return work(ctx, f.repos)
}

type fakeInventory struct {
	items []domain.Inventory
	saves int
}
func (f *fakeInventory) LockRange(context.Context, domain.HotelID, domain.RoomTypeID, time.Time, time.Time) ([]domain.Inventory, error) {
	return append([]domain.Inventory(nil), f.items...), nil
}
func (f *fakeInventory) SaveInventory(_ context.Context, item *domain.Inventory) error {
	f.saves++
	for i := range f.items {
		if f.items[i].Date.Equal(item.Date) {
			f.items[i] = *item
		}
	}
	return nil
}

type fakeReservations struct {
	byBooking map[domain.BookingID]*domain.Reservation
	byID map[domain.ReservationID]*domain.Reservation
	creates int
	saves int
}
func (f *fakeReservations) FindByBookingID(_ context.Context, id domain.BookingID) (*domain.Reservation, error) {
	item := f.byBooking[id]
	if item == nil { return nil, repository.ErrNotFound }
	copy := *item
	return &copy, nil
}
func (f *fakeReservations) LockByID(_ context.Context, id domain.ReservationID) (*domain.Reservation, error) {
	item := f.byID[id]
	if item == nil { return nil, repository.ErrNotFound }
	copy := *item
	return &copy, nil
}
func (f *fakeReservations) Create(_ context.Context, item domain.Reservation) error {
	f.creates++
	copy := item
	f.byBooking[item.BookingID] = &copy
	f.byID[item.ID] = &copy
	return nil
}
func (f *fakeReservations) SaveReservation(_ context.Context, item domain.Reservation) error {
	f.saves++
	copy := item
	f.byBooking[item.BookingID] = &copy
	f.byID[item.ID] = &copy
	return nil
}
func (*fakeReservations) AddInventory(context.Context, []domain.ReservationInventory) error { return nil }
func (*fakeReservations) ListInventory(context.Context, domain.ReservationID) ([]domain.ReservationInventory, error) {
	return nil, nil
}

type fakeOutbox struct{}
func (fakeOutbox) Add(context.Context, domain.OutboxEvent) error { return nil }

func fixture(t *testing.T) (*Service, *fakeInventory, *fakeReservations, ReserveInventoryInput) {
	t.Helper()
	checkIn := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	inventory := &fakeInventory{items: []domain.Inventory{
		{HotelID:"h1", RoomTypeID:"r1", Date:checkIn, TotalQuantity:3},
		{HotelID:"h1", RoomTypeID:"r1", Date:checkIn.AddDate(0,0,1), TotalQuantity:3},
	}}
	reservations := &fakeReservations{byBooking:map[domain.BookingID]*domain.Reservation{}, byID:map[domain.ReservationID]*domain.Reservation{}}
	repos := repository.Repositories{Inventory:inventory, Reservation:reservations, Outbox:fakeOutbox{}}
	service := NewService(NewTransactionBoundary(fakeTransactions{repos:repos}), fixedIDs{id:"res-1"}, fixedClock{now:checkIn.Add(-time.Hour)})
	input := ReserveInventoryInput{
		BookingID:"b1", HotelID:"h1", RoomTypeID:"r1",
		CheckIn:checkIn, CheckOut:checkIn.AddDate(0,0,2), Quantity:2, HoldTTL:15*time.Minute,
	}
	return service, inventory, reservations, input
}

func TestReserveInventoryReplayDoesNotMutateInventoryTwice(t *testing.T) {
	service, inventory, reservations, input := fixture(t)
	first, err := service.ReserveInventory(context.Background(), input)
	if err != nil { t.Fatalf("first reserve: %v", err) }
	second, err := service.ReserveInventory(context.Background(), input)
	if err != nil { t.Fatalf("replayed reserve: %v", err) }
	if first.ReservationID != second.ReservationID { t.Fatalf("replay returned a different reservation") }
	if inventory.saves != 2 || reservations.creates != 1 {
		t.Fatalf("replay mutated state: inventory saves=%d creates=%d", inventory.saves, reservations.creates)
	}
	for _, item := range inventory.items {
		if item.HeldQuantity != 2 { t.Fatalf("held quantity=%d, want 2", item.HeldQuantity) }
	}
}

func TestReserveInventoryRejectsSameBookingWithDifferentRequest(t *testing.T) {
	service, _, _, input := fixture(t)
	if _, err := service.ReserveInventory(context.Background(), input); err != nil { t.Fatal(err) }
	input.Quantity = 1
	if _, err := service.ReserveInventory(context.Background(), input); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestConfirmReservationReplayMovesCountersOnce(t *testing.T) {
	service, inventory, reservations, input := fixture(t)
	reserved, err := service.ReserveInventory(context.Background(), input)
	if err != nil { t.Fatal(err) }
	inventory.saves = 0
	if _, err := service.ConfirmReservation(context.Background(), reserved.ReservationID); err != nil { t.Fatal(err) }
	if _, err := service.ConfirmReservation(context.Background(), reserved.ReservationID); err != nil { t.Fatal(err) }
	if inventory.saves != 2 || reservations.saves != 1 {
		t.Fatalf("confirm replay mutated state: inventory saves=%d reservation saves=%d", inventory.saves, reservations.saves)
	}
	for _, item := range inventory.items {
		if item.HeldQuantity != 0 || item.BookedQuantity != 2 {
			t.Fatalf("unexpected counters: held=%d booked=%d", item.HeldQuantity, item.BookedQuantity)
		}
	}
}

func TestReleaseReservationReplayMovesCountersOnce(t *testing.T) {
	service, inventory, reservations, input := fixture(t)
	reserved, err := service.ReserveInventory(context.Background(), input)
	if err != nil { t.Fatal(err) }
	inventory.saves = 0
	if _, err := service.ReleaseReservation(context.Background(), reserved.ReservationID); err != nil { t.Fatal(err) }
	if _, err := service.ReleaseReservation(context.Background(), reserved.ReservationID); err != nil { t.Fatal(err) }
	if inventory.saves != 2 || reservations.saves != 1 {
		t.Fatalf("release replay mutated state: inventory saves=%d reservation saves=%d", inventory.saves, reservations.saves)
	}
	for _, item := range inventory.items {
		if item.HeldQuantity != 0 || item.BookedQuantity != 0 {
			t.Fatalf("unexpected counters: held=%d booked=%d", item.HeldQuantity, item.BookedQuantity)
		}
	}
}

func TestCheckAvailabilityUsesLowestNight(t *testing.T) {
	service, inventory, _, input := fixture(t)
	inventory.items[0].HeldQuantity = 1
	inventory.items[1].HeldQuantity = 2
	result, err := service.CheckAvailability(context.Background(), CheckAvailabilityInput{
		HotelID:input.HotelID, RoomTypeID:input.RoomTypeID,
		CheckIn:input.CheckIn, CheckOut:input.CheckOut, Quantity:2,
	})
	if err != nil { t.Fatal(err) }
	if result.Available || result.AvailableQuantity != 1 {
		t.Fatalf("result=%+v, want unavailable with one room", result)
	}
}
