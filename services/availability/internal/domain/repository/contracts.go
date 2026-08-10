package repository

import (
	"context"
	"errors"
	"time"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
)

var (
	ErrNotFound        = errors.New("repository: not found")
	ErrConcurrentWrite = errors.New("repository: concurrent write")
)

type InventoryRepository interface {
	LockRange(
		ctx context.Context,
		hotelID domain.HotelID,
		roomTypeID domain.RoomTypeID,
		checkIn time.Time,
		checkOut time.Time,
	) ([]domain.Inventory, error)
	SaveInventory(ctx context.Context, inventory *domain.Inventory) error
}

type ReservationRepository interface {
	FindByBookingID(ctx context.Context, bookingID domain.BookingID) (*domain.Reservation, error)
	LockByID(ctx context.Context, reservationID domain.ReservationID) (*domain.Reservation, error)
	Create(ctx context.Context, reservation domain.Reservation) error
	SaveReservation(ctx context.Context, reservation domain.Reservation) error
	AddInventory(ctx context.Context, items []domain.ReservationInventory) error
	ListInventory(ctx context.Context, reservationID domain.ReservationID) ([]domain.ReservationInventory, error)
}

type OutboxRepository interface {
	Add(ctx context.Context, event domain.OutboxEvent) error
}

type Repositories struct {
	Inventory   InventoryRepository
	Reservation ReservationRepository
	Outbox      OutboxRepository
}

type TransactionWork func(ctx context.Context, repositories Repositories) error

type TransactionManager interface {
	WithinTransaction(ctx context.Context, work TransactionWork) error
}
