package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct {
	db DBTX
}

func NewStore(db DBTX) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, booking *domain.Booking) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO bookings
		(id,user_id,hotel_id,room_type_id,check_in,check_out,guest_count,room_quantity,status,
		 reservation_id,payment_id,version,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),$12,$13,$14)`,
		booking.ID, booking.UserID, booking.HotelID, booking.RoomTypeID,
		booking.CheckIn, booking.CheckOut, booking.GuestCount, booking.RoomQuantity, booking.Status,
		booking.ReservationID, booking.PaymentID, booking.Version, booking.CreatedAt, booking.UpdatedAt,
	)
	return err
}

func (s *Store) Find(ctx context.Context, id string) (*domain.Booking, error) {
	return s.findBooking(ctx, id, false)
}

func (s *Store) Lock(ctx context.Context, id string) (*domain.Booking, error) {
	return s.findBooking(ctx, id, true)
}

func (s *Store) findBooking(ctx context.Context, id string, lock bool) (*domain.Booking, error) {
	query := `SELECT id,user_id,hotel_id,room_type_id,check_in,check_out,guest_count,room_quantity,status,
		COALESCE(reservation_id,''),COALESCE(payment_id,''),version,created_at,updated_at
		FROM bookings WHERE id=$1`
	if lock {
		query += " FOR UPDATE"
	}
	var booking domain.Booking
	if err := s.db.QueryRowContext(ctx, query, id).Scan(
		&booking.ID, &booking.UserID, &booking.HotelID, &booking.RoomTypeID,
		&booking.CheckIn, &booking.CheckOut, &booking.GuestCount, &booking.RoomQuantity,
		&booking.Status, &booking.ReservationID, &booking.PaymentID, &booking.Version,
		&booking.CreatedAt, &booking.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &booking, nil
}

func (s *Store) Save(ctx context.Context, booking *domain.Booking) error {
	result, err := s.db.ExecContext(ctx, `UPDATE bookings SET
		status=$1,reservation_id=NULLIF($2,''),payment_id=NULLIF($3,''),
		version=version+1,updated_at=$4 WHERE id=$5 AND version=$6`,
		booking.Status, booking.ReservationID, booking.PaymentID,
		booking.UpdatedAt, booking.ID, booking.Version,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return domain.ErrConcurrentWrite
	}
	booking.Version++
	return nil
}

func (s *Store) CreatePriceSnapshot(ctx context.Context, snapshot *domain.PriceSnapshot) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO booking_price_snapshots
		(booking_id,quote_id,currency,subtotal_minor,tax_minor,service_fee_minor,discount_minor,total_minor,
		 pricing_version,quoted_at,quote_expires_at,accepted_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		snapshot.BookingID, snapshot.QuoteID, snapshot.Currency,
		snapshot.SubtotalMinor, snapshot.TaxMinor, snapshot.ServiceFeeMinor,
		snapshot.DiscountMinor, snapshot.TotalMinor, snapshot.PricingVersion,
		snapshot.QuotedAt, snapshot.QuoteExpiresAt, snapshot.AcceptedAt,
	)
	return err
}

func (s *Store) FindPriceSnapshotByBookingID(ctx context.Context, bookingID string) (*domain.PriceSnapshot, error) {
	var snapshot domain.PriceSnapshot
	if err := s.db.QueryRowContext(ctx, `SELECT
		booking_id,quote_id,currency,subtotal_minor,tax_minor,service_fee_minor,discount_minor,total_minor,
		pricing_version,quoted_at,quote_expires_at,accepted_at
		FROM booking_price_snapshots WHERE booking_id=$1`, bookingID).Scan(
		&snapshot.BookingID, &snapshot.QuoteID, &snapshot.Currency,
		&snapshot.SubtotalMinor, &snapshot.TaxMinor, &snapshot.ServiceFeeMinor,
		&snapshot.DiscountMinor, &snapshot.TotalMinor, &snapshot.PricingVersion,
		&snapshot.QuotedAt, &snapshot.QuoteExpiresAt, &snapshot.AcceptedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &snapshot, nil
}

func (s *Store) CreateSaga(ctx context.Context, saga *domain.BookingSaga) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO booking_sagas
		(id,booking_id,state,reservation_id,payment_id,last_error_code,last_error_message,retry_count,next_retry_at,version,created_at,updated_at)
		VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9,$10,$11,$12)`,
		saga.ID, saga.BookingID, saga.State, saga.ReservationID, saga.PaymentID,
		saga.LastErrorCode, saga.LastErrorMessage, saga.RetryCount, saga.NextRetryAt,
		saga.Version, saga.CreatedAt, saga.UpdatedAt,
	)
	return err
}

func (s *Store) LockSagaByBookingID(ctx context.Context, bookingID string) (*domain.BookingSaga, error) {
	var saga domain.BookingSaga
	if err := s.db.QueryRowContext(ctx, `SELECT
		id,booking_id,state,COALESCE(reservation_id,''),COALESCE(payment_id,''),
		COALESCE(last_error_code,''),COALESCE(last_error_message,''),retry_count,next_retry_at,
		version,created_at,updated_at
		FROM booking_sagas WHERE booking_id=$1 FOR UPDATE`, bookingID).Scan(
		&saga.ID, &saga.BookingID, &saga.State, &saga.ReservationID, &saga.PaymentID,
		&saga.LastErrorCode, &saga.LastErrorMessage, &saga.RetryCount, &saga.NextRetryAt,
		&saga.Version, &saga.CreatedAt, &saga.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &saga, nil
}

func (s *Store) SaveSaga(ctx context.Context, saga *domain.BookingSaga) error {
	result, err := s.db.ExecContext(ctx, `UPDATE booking_sagas SET
		state=$1,reservation_id=NULLIF($2,''),payment_id=NULLIF($3,''),
		last_error_code=NULLIF($4,''),last_error_message=NULLIF($5,''),
		retry_count=$6,next_retry_at=$7,version=version+1,updated_at=$8
		WHERE id=$9 AND version=$10`,
		saga.State, saga.ReservationID, saga.PaymentID, saga.LastErrorCode, saga.LastErrorMessage,
		saga.RetryCount, saga.NextRetryAt, saga.UpdatedAt, saga.ID, saga.Version,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return domain.ErrConcurrentWrite
	}
	 saga.Version++
	return nil
}

func (s *Store) ClaimIdempotency(ctx context.Context, record *domain.IdempotencyRecord) error {
	result, err := s.db.ExecContext(ctx, `INSERT INTO booking_idempotency
		(id,idempotency_key,request_hash,booking_id,status,response_payload,created_at,updated_at,expires_at)
		VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9)
		ON CONFLICT(idempotency_key) DO NOTHING`,
		record.ID, record.Key, record.RequestHash, record.BookingID, record.Status,
		nullableJSON(record.ResponsePayload), record.CreatedAt, record.UpdatedAt, record.ExpiresAt,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return domain.ErrIdempotencyConflict
	}
	return nil
}

func (s *Store) FindIdempotencyByKey(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	var record domain.IdempotencyRecord
	var response []byte
	if err := s.db.QueryRowContext(ctx, `SELECT
		id,idempotency_key,request_hash,COALESCE(booking_id::text,''),status,
		COALESCE(response_payload::text,'')::bytea,created_at,updated_at,expires_at
		FROM booking_idempotency WHERE idempotency_key=$1`, key).Scan(
		&record.ID, &record.Key, &record.RequestHash, &record.BookingID, &record.Status,
		&response, &record.CreatedAt, &record.UpdatedAt, &record.ExpiresAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	record.ResponsePayload = response
	return &record, nil
}

func (s *Store) SaveIdempotency(ctx context.Context, record *domain.IdempotencyRecord) error {
	result, err := s.db.ExecContext(ctx, `UPDATE booking_idempotency SET
		booking_id=NULLIF($1,'')::uuid,status=$2,response_payload=$3,updated_at=$4,expires_at=$5
		WHERE id=$6`,
		record.BookingID, record.Status, nullableJSON(record.ResponsePayload),
		record.UpdatedAt, record.ExpiresAt, record.ID,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) AddOutbox(ctx context.Context, event *domain.OutboxEvent) error {
	if !json.Valid(event.Payload) {
		return fmt.Errorf("invalid outbox payload")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO booking_outbox_events
		(id,aggregate_type,aggregate_id,event_type,event_version,payload,status,retry_count,next_retry_at,created_at,published_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		event.ID, event.AggregateType, event.AggregateID, event.EventType, event.EventVersion,
		event.Payload, event.Status, event.RetryCount, event.NextRetryAt, event.CreatedAt, event.PublishedAt,
	)
	return err
}

func nullableJSON(payload []byte) any {
	if len(payload) == 0 {
		return nil
	}
	return payload
}

type Transactor struct {
	DB *sql.DB
}

func (t Transactor) WithinTransaction(ctx context.Context, work func(context.Context, domain.Repositories) error) error {
	if work == nil {
		return errors.New("postgres: transaction work is required")
	}
	tx, err := t.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	store := NewStore(tx)
	repositories := domain.Repositories{
		Bookings:       bookingRepo{store},
		PriceSnapshots: priceRepo{store},
		Sagas:          sagaRepo{store},
		Idempotency:    idempotencyRepo{store},
		Outbox:         outboxRepo{store},
	}
	if err := work(ctx, repositories); err != nil {
		return err
	}
	return tx.Commit()
}

type bookingRepo struct{ *Store }

type priceRepo struct{ *Store }

func (r priceRepo) Create(ctx context.Context, snapshot *domain.PriceSnapshot) error {
	return r.CreatePriceSnapshot(ctx, snapshot)
}

func (r priceRepo) FindByBookingID(ctx context.Context, bookingID string) (*domain.PriceSnapshot, error) {
	return r.FindPriceSnapshotByBookingID(ctx, bookingID)
}

type sagaRepo struct{ *Store }

func (r sagaRepo) Create(ctx context.Context, saga *domain.BookingSaga) error {
	return r.CreateSaga(ctx, saga)
}

func (r sagaRepo) LockByBookingID(ctx context.Context, bookingID string) (*domain.BookingSaga, error) {
	return r.LockSagaByBookingID(ctx, bookingID)
}

func (r sagaRepo) Save(ctx context.Context, saga *domain.BookingSaga) error {
	return r.SaveSaga(ctx, saga)
}

type idempotencyRepo struct{ *Store }

func (r idempotencyRepo) Claim(ctx context.Context, record *domain.IdempotencyRecord) error {
	return r.ClaimIdempotency(ctx, record)
}

func (r idempotencyRepo) FindByKey(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	return r.FindIdempotencyByKey(ctx, key)
}

func (r idempotencyRepo) Save(ctx context.Context, record *domain.IdempotencyRecord) error {
	return r.SaveIdempotency(ctx, record)
}

type outboxRepo struct{ *Store }

func (r outboxRepo) Add(ctx context.Context, event *domain.OutboxEvent) error {
	return r.AddOutbox(ctx, event)
}

var (
	_ domain.BookingRepository       = bookingRepo{}
	_ domain.PriceSnapshotRepository = priceRepo{}
	_ domain.SagaRepository          = sagaRepo{}
	_ domain.IdempotencyRepository   = idempotencyRepo{}
	_ domain.OutboxRepository        = outboxRepo{}
	_ domain.TransactionManager      = Transactor{}
)
