package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/domain/repository"
)

const lockInventoryRangeSQL = `
SELECT
    hotel_id,
    room_type_id,
    inventory_date,
    total_quantity,
    held_quantity,
    booked_quantity,
    version,
    created_at,
    updated_at
FROM room_inventory
WHERE hotel_id = $1
  AND room_type_id = $2
  AND inventory_date >= $3
  AND inventory_date < $4
ORDER BY inventory_date
FOR UPDATE`

const saveInventorySQL = `
UPDATE room_inventory
SET total_quantity = $4,
    held_quantity = $5,
    booked_quantity = $6,
    version = version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE hotel_id = $1
  AND room_type_id = $2
  AND inventory_date = $3
  AND version = $7
RETURNING version, updated_at`

const findReservationByBookingIDSQL = `
SELECT
    id,
    booking_id,
    hotel_id,
    room_type_id,
    check_in,
    check_out,
    quantity,
    status,
    expires_at,
    created_at,
    updated_at
FROM reservations
WHERE booking_id = $1`

const lockReservationByIDSQL = `
SELECT
    id,
    booking_id,
    hotel_id,
    room_type_id,
    check_in,
    check_out,
    quantity,
    status,
    expires_at,
    created_at,
    updated_at
FROM reservations
WHERE id = $1
FOR UPDATE`

const createReservationSQL = `
INSERT INTO reservations (
    id,
    booking_id,
    hotel_id,
    room_type_id,
    check_in,
    check_out,
    quantity,
    status,
    expires_at,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

const saveReservationSQL = `
UPDATE reservations
SET status = $2,
    expires_at = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING updated_at`

const addReservationInventorySQL = `
INSERT INTO reservation_inventory (
    reservation_id,
    inventory_date,
    quantity
) VALUES ($1, $2, $3)`

const listReservationInventorySQL = `
SELECT reservation_id, inventory_date, quantity
FROM reservation_inventory
WHERE reservation_id = $1
ORDER BY inventory_date`

const addOutboxEventSQL = `
INSERT INTO availability_outbox_events (
    id,
    aggregate_type,
    aggregate_id,
    aggregate_version,
    event_type,
    payload,
    available_at,
    created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type rowScanner interface {
	Scan(dest ...any) error
}

// Store implements all Availability repository ports over one query target.
// The target is a *sql.Tx when created by Transactor, so all calls supplied to
// one usecase callback share exactly one PostgreSQL transaction.
type Store struct {
	queries queryer
}

func NewStore(queries queryer) *Store {
	return &Store{queries: queries}
}

var (
	_ repository.InventoryRepository   = (*Store)(nil)
	_ repository.ReservationRepository = (*Store)(nil)
	_ repository.OutboxRepository      = (*Store)(nil)
	_ repository.TransactionManager    = (*Transactor)(nil)
)

type Transactor struct {
	db *sql.DB
}

func NewTransactor(db *sql.DB) *Transactor {
	return &Transactor{db: db}
}

func (t *Transactor) WithinTransaction(
	ctx context.Context,
	work repository.TransactionWork,
) error {
	if work == nil {
		return errors.New("postgres: transaction work is required")
	}

	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	store := NewStore(tx)
	repositories := repository.Repositories{
		Inventory:   store,
		Reservation: store,
		Outbox:      store,
	}
	if err := work(ctx, repositories); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (s *Store) LockRange(
	ctx context.Context,
	hotelID domain.HotelID,
	roomTypeID domain.RoomTypeID,
	checkIn,
	checkOut time.Time,
) ([]domain.Inventory, error) {
	rows, err := s.queries.QueryContext(
		ctx,
		lockInventoryRangeSQL,
		hotelID,
		roomTypeID,
		checkIn,
		checkOut,
	)
	if err != nil {
		return nil, fmt.Errorf("lock inventory range: %w", err)
	}
	defer rows.Close()

	var inventory []domain.Inventory
	for rows.Next() {
		var item domain.Inventory
		if err := rows.Scan(
			&item.HotelID,
			&item.RoomTypeID,
			&item.Date,
			&item.TotalQuantity,
			&item.HeldQuantity,
			&item.BookedQuantity,
			&item.Version,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan inventory: %w", err)
		}
		inventory = append(inventory, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inventory: %w", err)
	}
	return inventory, nil
}

func (s *Store) SaveInventory(ctx context.Context, inventory *domain.Inventory) error {
	if inventory == nil {
		return errors.New("postgres: inventory is required")
	}

	err := s.queries.QueryRowContext(
		ctx,
		saveInventorySQL,
		inventory.HotelID,
		inventory.RoomTypeID,
		inventory.Date,
		inventory.TotalQuantity,
		inventory.HeldQuantity,
		inventory.BookedQuantity,
		inventory.Version,
	).Scan(&inventory.Version, &inventory.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrConcurrentWrite
	}
	if err != nil {
		return fmt.Errorf("save inventory: %w", err)
	}
	return nil
}

func (s *Store) FindByBookingID(
	ctx context.Context,
	bookingID domain.BookingID,
) (*domain.Reservation, error) {
	reservation, err := scanReservation(
		s.queries.QueryRowContext(ctx, findReservationByBookingIDSQL, bookingID),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find reservation by booking id: %w", err)
	}
	return reservation, nil
}

func (s *Store) LockByID(
	ctx context.Context,
	reservationID domain.ReservationID,
) (*domain.Reservation, error) {
	reservation, err := scanReservation(
		s.queries.QueryRowContext(ctx, lockReservationByIDSQL, reservationID),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock reservation: %w", err)
	}
	return reservation, nil
}

func (s *Store) Create(ctx context.Context, reservation domain.Reservation) error {
	_, err := s.queries.ExecContext(
		ctx,
		createReservationSQL,
		reservation.ID,
		reservation.BookingID,
		reservation.HotelID,
		reservation.RoomTypeID,
		reservation.CheckIn,
		reservation.CheckOut,
		reservation.Quantity,
		reservation.Status,
		reservation.ExpiresAt,
		reservation.CreatedAt,
		reservation.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create reservation: %w", err)
	}
	return nil
}

func (s *Store) SaveReservation(
	ctx context.Context,
	reservation domain.Reservation,
) error {
	err := s.queries.QueryRowContext(
		ctx,
		saveReservationSQL,
		reservation.ID,
		reservation.Status,
		reservation.ExpiresAt,
	).Scan(&reservation.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("save reservation: %w", err)
	}
	return nil
}

func (s *Store) AddInventory(
	ctx context.Context,
	items []domain.ReservationInventory,
) error {
	for _, item := range items {
		if _, err := s.queries.ExecContext(
			ctx,
			addReservationInventorySQL,
			item.ReservationID,
			item.Date,
			item.Quantity,
		); err != nil {
			return fmt.Errorf("add reservation inventory: %w", err)
		}
	}
	return nil
}

func (s *Store) ListInventory(
	ctx context.Context,
	reservationID domain.ReservationID,
) ([]domain.ReservationInventory, error) {
	rows, err := s.queries.QueryContext(
		ctx,
		listReservationInventorySQL,
		reservationID,
	)
	if err != nil {
		return nil, fmt.Errorf("list reservation inventory: %w", err)
	}
	defer rows.Close()

	var items []domain.ReservationInventory
	for rows.Next() {
		var item domain.ReservationInventory
		if err := rows.Scan(&item.ReservationID, &item.Date, &item.Quantity); err != nil {
			return nil, fmt.Errorf("scan reservation inventory: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reservation inventory: %w", err)
	}
	return items, nil
}

func (s *Store) Add(ctx context.Context, event domain.OutboxEvent) error {
	_, err := s.queries.ExecContext(
		ctx,
		addOutboxEventSQL,
		event.ID,
		event.AggregateType,
		event.AggregateID,
		event.AggregateVersion,
		event.EventType,
		string(event.Payload),
		event.AvailableAt,
		event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("add outbox event: %w", err)
	}
	return nil
}

func scanReservation(row rowScanner) (*domain.Reservation, error) {
	var reservation domain.Reservation
	var expiresAt sql.NullTime
	if err := row.Scan(
		&reservation.ID,
		&reservation.BookingID,
		&reservation.HotelID,
		&reservation.RoomTypeID,
		&reservation.CheckIn,
		&reservation.CheckOut,
		&reservation.Quantity,
		&reservation.Status,
		&expiresAt,
		&reservation.CreatedAt,
		&reservation.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		reservation.ExpiresAt = &expiresAt.Time
	}
	return &reservation, nil
}
