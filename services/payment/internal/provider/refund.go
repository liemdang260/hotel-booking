package provider

import (
	"context"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
)

type RefundRequest struct {
	RefundID          string
	PaymentID         string
	BookingID         string
	IdempotencyKey    string
	AmountMinor       int64
	Currency          string
}

type RefundResult struct {
	Outcome             domain.AttemptOutcome
	ProviderRequestRef  string
	ProviderReference   string
	FailureCode         string
	RawOutcome          string
}

// RefundProvider is implemented in infrastructure. A timeout or lost response
// must be mapped to AttemptUnknown rather than a definitive failure.
type RefundProvider interface {
	Refund(context.Context, RefundRequest) RefundResult
	GetRefund(context.Context, RefundRequest) RefundResult
}
