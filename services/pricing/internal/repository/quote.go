package repository

import (
	"context"

	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
)

type QuoteRepository interface {
	// Insert is the only quote write operation. Quotes are immutable after insert.
	Insert(context.Context, domain.Quote) error
	Get(context.Context, string) (domain.Quote, error)
}

type RatePlanRepository interface {
	Current(context.Context, domain.QuoteInput) (domain.RatePlan, error)
}
