package provider

import (
	"context"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
)

type LookupRequest struct {
	PaymentID string
	IdempotencyKey string
	ProviderReference string
}

type LookupResult struct {
	Outcome domain.AttemptOutcome
	ProviderReference string
	FailureCode string
	RawOutcome string
}

// PaymentLookup reads provider truth. Infrastructure maps timeouts and responses
// without a definitive provider state to AttemptUnknown.
type PaymentLookup interface {
	GetPayment(context.Context, LookupRequest) LookupResult
}
