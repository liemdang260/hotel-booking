package provider

import (
	"context"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
)

type ChargeRequest struct {
	PaymentID string
	BookingID string
	IdempotencyKey string
	AmountMinor int64
	Currency string
	PaymentMethodRef string
}

type ChargeResult struct {
	Outcome domain.AttemptOutcome
	ProviderRequestRef string
	ProviderReference string
	FailureCode string
	RawOutcome string
}

// PaymentProvider is implemented only in infrastructure. Provider SDK types must
// not cross this boundary. Ambiguous transport outcomes are returned as
// AttemptUnknown, never as a deterministic decline.
type PaymentProvider interface {
	Charge(context.Context, ChargeRequest) ChargeResult
}
