package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/usecase"
)

const lockExpiredReservationsSQL = `
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
WHERE status = 'HELD'
  AND expires_at <= $1
ORDER BY expires_at, id
FOR UPDATE SKIP LOCKED
LIMIT $2`

type ExpirationStore struct {
	queries queryer
}

func NewExpirationStore(queries queryer) *ExpirationStore {
	return &ExpirationStore{queries: queries}
}

func (s *ExpirationStore) LockExpired(ctx context.Context, now time.Time, limit int) ([]domain.Reservation, error) {
	rows, err := s.queries.QueryContext(ctx, lockExpiredReservationsSQL, now, limit)
	if err != nil {
		return nil, fmt.Errorf("lock expired reservations: %w", err)
	}
	defer rows.Close()

	reservations := make([]domain.Reservation, 0, limit)
	for rows.Next() {
		reservation, err := scanReservation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan expired reservation: %w", err)
		}
		reservations = append(reservations, *reservation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired reservations: %w", err)
	}
	return reservations, nil
}

type ExpirationTransactor struct {
	db *sql.DB
}

func NewExpirationTransactor(db *sql.DB) *ExpirationTransactor {
	return &ExpirationTransactor{db: db}
}

func (t *ExpirationTransactor) WithinExpirationTransaction(ctx context.Context, work usecase.ExpirationTransactionWork) error {
	if work == nil {
		return errors.New("postgres: expiration transaction work is required")
	}
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin expiration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	store := NewStore(tx)
	repositories := usecase.ExpirationRepositories{
		Inventory: store,
		Reservation: store,
		Expired: NewExpirationStore(tx),
		Outbox: store,
	}
	if err := work(ctx, repositories); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit expiration transaction: %w", err)
	}
	return nil
}

var (
	_ usecase.ExpiredReservationRepository = (*ExpirationStore)(nil)
	_ usecase.ExpirationTransactionManager = (*ExpirationTransactor)(nil)
)
