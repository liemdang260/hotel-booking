package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
	"github.com/liemdang260/hotel-booking/services/pricing/internal/repository"
)

type QuoteRepository struct{db *sql.DB}
func NewQuoteRepository(db *sql.DB)*QuoteRepository{return &QuoteRepository{db:db}}

func(r *QuoteRepository)Insert(ctx context.Context,q domain.Quote)error{
	_,err:=r.db.ExecContext(ctx,`INSERT INTO quotes (
 id,hotel_id,room_type_id,check_in,check_out,guest_count,room_quantity,
 subtotal_minor,tax_minor,service_fee_minor,discount_minor,total_minor,
 currency,pricing_version,cancellation_policy_code,cancellation_policy_version,
 free_cancel_until,refund_basis_points,cancellation_fee_minor,created_at,expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
	q.ID,q.Input.HotelID,q.Input.RoomTypeID,q.Input.CheckIn.Time(),q.Input.CheckOut.Time(),q.Input.GuestCount,q.Input.RoomQuantity,
	q.Price.SubtotalMinor,q.Price.TaxMinor,q.Price.ServiceFeeMinor,q.Price.DiscountMinor,q.Price.TotalMinor,q.Currency,q.PricingVersion,
	q.CancellationPolicy.PolicyCode,q.CancellationPolicy.PolicyVersion,q.CancellationPolicy.FreeCancelUntil,q.CancellationPolicy.RefundBasisPoints,q.CancellationPolicy.CancellationFeeMinor,q.CreatedAt,q.ExpiresAt)
	return err
}

func(r *QuoteRepository)Get(ctx context.Context,id string)(domain.Quote,error){
	var q domain.Quote;var in,out sql.NullTime
	err:=r.db.QueryRowContext(ctx,`SELECT id,hotel_id,room_type_id,check_in,check_out,guest_count,room_quantity,
 subtotal_minor,tax_minor,service_fee_minor,discount_minor,total_minor,currency,pricing_version,
 cancellation_policy_code,cancellation_policy_version,free_cancel_until,refund_basis_points,cancellation_fee_minor,
 created_at,expires_at FROM quotes WHERE id=$1`,id).Scan(
	&q.ID,&q.Input.HotelID,&q.Input.RoomTypeID,&in,&out,&q.Input.GuestCount,&q.Input.RoomQuantity,
	&q.Price.SubtotalMinor,&q.Price.TaxMinor,&q.Price.ServiceFeeMinor,&q.Price.DiscountMinor,&q.Price.TotalMinor,&q.Currency,&q.PricingVersion,
	&q.CancellationPolicy.PolicyCode,&q.CancellationPolicy.PolicyVersion,&q.CancellationPolicy.FreeCancelUntil,&q.CancellationPolicy.RefundBasisPoints,&q.CancellationPolicy.CancellationFeeMinor,
	&q.CreatedAt,&q.ExpiresAt)
	if errors.Is(err,sql.ErrNoRows){return domain.Quote{},repository.ErrQuoteNotFound};if err!=nil{return domain.Quote{},err}
	if !in.Valid||!out.Valid{return domain.Quote{},domain.ErrInvalidStay}
	q.Input.CheckIn=domain.Date{Year:in.Time.Year(),Month:in.Time.Month(),Day:in.Time.Day()};q.Input.CheckOut=domain.Date{Year:out.Time.Year(),Month:out.Time.Month(),Day:out.Time.Day()}
	return q,nil
}
