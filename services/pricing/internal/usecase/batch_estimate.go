package usecase

import (
	"context"

	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
	"github.com/liemdang260/hotel-booking/services/pricing/internal/repository"
)

const maxBatchEstimateItems = 100

type EstimateInput struct {
	HotelID string
	RoomTypeID string
	CheckIn domain.Date
	CheckOut domain.Date
	GuestCount int
	RoomQuantity int
}

type Estimate struct {
	HotelID string
	RoomTypeID string
	TotalMinor int64
	Currency string
	PricingVersion string
}

type BatchEstimateUsecase struct {
	rates repository.RatePlanRepository
}

func NewBatchEstimateUsecase(rates repository.RatePlanRepository) *BatchEstimateUsecase {
	return &BatchEstimateUsecase{rates: rates}
}

// Execute calculates advisory values only. It deliberately does not call the
// QuoteRepository, allocate quote IDs, or create accepted pricing terms.
func (u *BatchEstimateUsecase) Execute(ctx context.Context, items []EstimateInput) ([]Estimate, error) {
	if len(items) == 0 || len(items) > maxBatchEstimateItems || u.rates == nil {
		return nil, domain.ErrInvalidParty
	}
	results := make([]Estimate, 0, len(items))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		input := domain.QuoteInput{
			HotelID: item.HotelID,
			RoomTypeID: item.RoomTypeID,
			CheckIn: item.CheckIn,
			CheckOut: item.CheckOut,
			GuestCount: item.GuestCount,
			RoomQuantity: item.RoomQuantity,
		}
		if err := input.Validate(); err != nil {
			return nil, err
		}
		plan, err := u.rates.Current(ctx, input)
		if err != nil {
			return nil, err
		}
		price, err := domain.CalculatePrice(input, plan)
		if err != nil {
			return nil, err
		}
		results = append(results, Estimate{
			HotelID: item.HotelID,
			RoomTypeID: item.RoomTypeID,
			TotalMinor: price.TotalMinor,
			Currency: plan.Currency,
			PricingVersion: plan.PricingVersion,
		})
	}
	return results, nil
}
