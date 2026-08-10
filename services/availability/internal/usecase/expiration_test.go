package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/domain/repository"
)

type expirationFixedClock struct{ now time.Time }
func (c expirationFixedClock) Now() time.Time { return c.now }

type expirationIDs struct{ next int }
func (g *expirationIDs) NewEventID() domain.EventID {
	g.next++
	return domain.EventID("event-" + string(rune('0'+g.next)))
}

type expirationInventory struct {
	items []domain.Inventory
	saves int
}
func (f *expirationInventory) LockRange(context.Context, domain.HotelID, domain.RoomTypeID, time.Time, time.Time) ([]domain.Inventory, error) {
	return append([]domain.Inventory(nil), f.items...), nil
}
func (f *expirationInventory) SaveInventory(_ context.Context, item *domain.Inventory) error {
	f.saves++
	for i := range f.items {
		if f.items[i].Date.Equal(item.Date) { f.items[i] = *item }
	}
	return nil
}

type expirationReservations struct {
	item domain.Reservation
	saves int
}
func (*expirationReservations) FindByBookingID(context.Context, domain.BookingID) (*domain.Reservation, error) { return nil, repository.ErrNotFound }
func (f *expirationReservations) LockByID(context.Context, domain.ReservationID) (*domain.Reservation, error) {
	copy := f.item
	return &copy, nil
}
func (*expirationReservations) Create(context.Context, domain.Reservation) error { return nil }
func (f *expirationReservations) SaveReservation(_ context.Context, item domain.Reservation) error {
	f.item = item
	f.saves++
	return nil
}
func (*expirationReservations) AddInventory(context.Context, []domain.ReservationInventory) error { return nil }
func (*expirationReservations) ListInventory(context.Context, domain.ReservationID) ([]domain.ReservationInventory, error) { return nil, nil }

type expirationClaimer struct{ reservations *expirationReservations }
func (c expirationClaimer) LockExpired(_ context.Context, now time.Time, limit int) ([]domain.Reservation, error) {
	if limit <= 0 || c.reservations.item.Status != domain.ReservationHeld || c.reservations.item.ExpiresAt.After(now) {
		return nil, nil
	}
	return []domain.Reservation{c.reservations.item}, nil
}

type expirationOutbox struct {
	events []domain.OutboxEvent
}
func (o *expirationOutbox) Add(_ context.Context, event domain.OutboxEvent) error {
	o.events = append(o.events, event)
	return nil
}

type expirationTx struct{ repos ExpirationRepositories }
func (t expirationTx) WithinExpirationTransaction(ctx context.Context, work ExpirationTransactionWork) error {
	return work(ctx, t.repos)
}

func TestExpirationReplayReleasesInventoryAndWritesOutboxOnce(t *testing.T) {
	now := time.Date(2026,9,1,12,0,0,0,time.UTC)
	checkIn := time.Date(2026,9,2,0,0,0,0,time.UTC)
	expiresAt := now.Add(-time.Minute)
	reservations := &expirationReservations{item:domain.Reservation{
		ID:"res-1", BookingID:"book-1", HotelID:"h1", RoomTypeID:"r1",
		CheckIn:checkIn, CheckOut:checkIn.AddDate(0,0,2), Quantity:2,
		Status:domain.ReservationHeld, ExpiresAt:&expiresAt,
	}}
	inventory := &expirationInventory{items:[]domain.Inventory{
		{HotelID:"h1",RoomTypeID:"r1",Date:checkIn,TotalQuantity:3,HeldQuantity:2},
		{HotelID:"h1",RoomTypeID:"r1",Date:checkIn.AddDate(0,0,1),TotalQuantity:3,HeldQuantity:2},
	}}
	outbox := &expirationOutbox{}
	repos := ExpirationRepositories{
		Inventory:inventory, Reservation:reservations,
		Expired:expirationClaimer{reservations:reservations}, Outbox:outbox,
	}
	usecase := NewExpireReservations(expirationTx{repos:repos}, &expirationIDs{}, expirationFixedClock{now:now}, 10)

	count, err := usecase.ExecuteBatch(context.Background(), 100)
	if err != nil { t.Fatalf("first expiration: %v", err) }
	replayed, err := usecase.ExecuteBatch(context.Background(), 100)
	if err != nil { t.Fatalf("replayed expiration: %v", err) }
	if count != 1 || replayed != 0 { t.Fatalf("counts=%d,%d want 1,0", count, replayed) }
	if inventory.saves != 2 || reservations.saves != 1 || len(outbox.events) != 1 {
		t.Fatalf("duplicate mutation: inventory=%d reservation=%d events=%d", inventory.saves, reservations.saves, len(outbox.events))
	}
	if reservations.item.Status != domain.ReservationExpired { t.Fatalf("status=%s", reservations.item.Status) }
	for _, item := range inventory.items {
		if item.HeldQuantity != 0 { t.Fatalf("held quantity=%d want 0", item.HeldQuantity) }
	}
	if outbox.events[0].EventType != "ReservationExpired" { t.Fatalf("event=%s", outbox.events[0].EventType) }
}

func TestExpirationRejectsBrokenHeldCounter(t *testing.T) {
	now := time.Date(2026,9,1,12,0,0,0,time.UTC)
	checkIn := time.Date(2026,9,2,0,0,0,0,time.UTC)
	expiresAt := now.Add(-time.Minute)
	reservations := &expirationReservations{item:domain.Reservation{
		ID:"res-1", BookingID:"book-1", HotelID:"h1", RoomTypeID:"r1",
		CheckIn:checkIn, CheckOut:checkIn.AddDate(0,0,1), Quantity:2,
		Status:domain.ReservationHeld, ExpiresAt:&expiresAt,
	}}
	inventory := &expirationInventory{items:[]domain.Inventory{
		{HotelID:"h1",RoomTypeID:"r1",Date:checkIn,TotalQuantity:3,HeldQuantity:1},
	}}
	repos := ExpirationRepositories{
		Inventory:inventory, Reservation:reservations,
		Expired:expirationClaimer{reservations:reservations}, Outbox:&expirationOutbox{},
	}
	usecase := NewExpireReservations(expirationTx{repos:repos}, &expirationIDs{}, expirationFixedClock{now:now}, 10)
	if _, err := usecase.ExecuteBatch(context.Background(), 10); err != ErrExpirationInvariant {
		t.Fatalf("expected invariant error, got %v", err)
	}
}
