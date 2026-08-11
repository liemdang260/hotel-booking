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

// CalculateRefund freezes the accepted commercial policy at evaluatedAt. It
// never consults current Pricing state and uses integer minor units only.
func (p CancellationPolicySnapshot) CalculateRefund(totalMinor int64,evaluatedAt time.Time)(int64,error){
	if err:=p.Validate();err!=nil{return 0,err}
	if totalMinor<0||evaluatedAt.IsZero(){return 0,fmt.Errorf("%w: invalid refund input",ErrInvalidPriceSnapshot)}
	refund:=totalMinor*int64(p.RefundBasisPoints)/10000-p.CancellationFeeMinor
	if evaluatedAt.After(p.FreeCancelUntil) && p.PolicyCode=="FLEXIBLE" { refund=0 }
	if refund<0{return 0,nil}
	if refund>totalMinor{return totalMinor,nil}
	return refund,nil
}
