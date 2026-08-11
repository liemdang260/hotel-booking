package domain

import (
	"fmt"
	"time"
)

type CancellationPolicySnapshot struct {
	BookingID string
	PolicyCode string
	PolicyVersion string
	FreeCancelUntil time.Time
	RefundBasisPoints int
	CancellationFeeMinor int64
	Currency string
	PricingVersion string
	CreatedAt time.Time
}

func (p CancellationPolicySnapshot) Validate() error {
	if p.BookingID==""||p.PolicyCode==""||p.PolicyVersion==""||p.FreeCancelUntil.IsZero()||len(p.Currency)!=3||p.PricingVersion==""||p.RefundBasisPoints<0||p.RefundBasisPoints>10000||p.CancellationFeeMinor<0||p.CreatedAt.IsZero(){
		return fmt.Errorf("%w: invalid cancellation policy snapshot",ErrInvalidPriceSnapshot)
	}
	return nil
}
