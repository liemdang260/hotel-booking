package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
	"github.com/liemdang260/hotel-booking/services/pricing/internal/repository"
)

type QuoteRepository struct {
	db *sql.DB
}

func NewQuoteRepository(db *sql.DB) *QuoteRepository {
	return &QuoteRepository{db: db}
}

func (r *QuoteRepository) Insert(ctx context.Context, quote domain.Quote) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO quotes (
			id, hotel_id, room_type_id, check_in, check_out, guest_count, room_quantity,
			subtotal_minor, tax_minor, service_fee_minor, discount_minor, total_minor,
			currency, pricing_version, created_at, expires_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12,
			$13, $14, $15, $16
		)`,
		quote.ID,
		quote.Input.HotelID,
		quote.Input.RoomTypeID,
		quote.Input.CheckIn.Time(),
		quote.Input.CheckOut.Time(),
		quote.Input.GuestCount,
		quote.Input.RoomQuantity,
		quote.Price.SubtotalMinor,
		quote.Price.TaxMinor,
		quote.Price.ServiceFeeMinor,
		quote.Price.DiscountMinor,
		quote.Price.TotalMinor,
		quote.Currency,
		quote.PricingVersion,
		quote.CreatedAt,
		quote.ExpiresAt,
	)
	return err
}

func (r *QuoteRepository) Get(ctx context.Context, quoteID string) (domain.Quote, error) {
	var quote domain.Quote
	var checkIn, checkOut sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT
			id, hotel_id, room_type_id, check_in, check_out, guest_count, room_quantity,
			subtotal_minor, tax_minor, service_fee_minor, discount_minor, total_minor,
			currency, pricing_version, created_at, expires_at
		FROM quotes
		WHERE id = $1`, quoteID,
	).Scan(
		&quote.ID,
		&quote.Input.HotelID,
		&quote.Input.RoomTypeID,
		&checkIn,
		&checkOut,
		&quote.Input.GuestCount,
		&quote.Input.RoomQuantity,
		&quote.Price.SubtotalMinor,
		&quote.Price.TaxMinor,
		&quote.Price.ServiceFeeMinor,
		&quote.Price.DiscountMinor,
		&quote.Price.TotalMinor,
		&quote.Currency,
		&quote.PricingVersion,
		&quote.CreatedAt,
		&quote.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Quote{}, repository.ErrQuoteNotFound
	}
	if err != nil {
		return domain.Quote{}, err
	}
	if !checkIn.Valid || !checkOut.Valid {
		return domain.Quote{}, domain.ErrInvalidStay
	}
	quote.Input.CheckIn = domain.Date{Year: checkIn.Time.Year(), Month: checkIn.Time.Month(), Day: checkIn.Time.Day()}
	quote.Input.CheckOut = domain.Date{Year: checkOut.Time.Year(), Month: checkOut.Time.Month(), Day: checkOut.Time.Day()}
	return quote, nil
}
