package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

func (s *Store) CreateCancellationPolicySnapshot(ctx context.Context, p *domain.CancellationPolicySnapshot) error {
	if p == nil { return domain.ErrInvalidPriceSnapshot }
	if err := p.Validate(); err != nil { return err }
	_, err := s.db.ExecContext(ctx, `INSERT INTO booking_cancellation_policies
(booking_id,policy_code,policy_version,free_cancel_until,refund_basis_points,cancellation_fee_minor,currency,pricing_version,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, p.BookingID,p.PolicyCode,p.PolicyVersion,p.FreeCancelUntil,p.RefundBasisPoints,p.CancellationFeeMinor,p.Currency,p.PricingVersion,p.CreatedAt)
	return err
}

func (s *Store) FindCancellationPolicySnapshot(ctx context.Context, bookingID string) (*domain.CancellationPolicySnapshot,error) {
	var p domain.CancellationPolicySnapshot
	err:=s.db.QueryRowContext(ctx,`SELECT booking_id::text,policy_code,policy_version,free_cancel_until,refund_basis_points,cancellation_fee_minor,currency,pricing_version,created_at FROM booking_cancellation_policies WHERE booking_id=$1`,bookingID).Scan(&p.BookingID,&p.PolicyCode,&p.PolicyVersion,&p.FreeCancelUntil,&p.RefundBasisPoints,&p.CancellationFeeMinor,&p.Currency,&p.PricingVersion,&p.CreatedAt)
	if errors.Is(err,sql.ErrNoRows){return nil,domain.ErrNotFound};if err!=nil{return nil,err};return &p,nil
}
