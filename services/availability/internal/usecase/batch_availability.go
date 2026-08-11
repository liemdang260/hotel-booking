package usecase

import (
	"context"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
)

const maxBatchAvailabilityItems = 100

type BatchCheckAvailabilityInput struct {
	Items []CheckAvailabilityInput
}

type BatchAvailabilityResult struct {
	HotelID          domain.HotelID
	RoomTypeID       domain.RoomTypeID
	AvailableQuantity int
	Available         bool
}

// BatchCheckAvailability is a read-only, fail-fast query. It provides one
// service call for a bounded candidate set and never creates a reservation.
func (s *Service) BatchCheckAvailability(ctx context.Context, input BatchCheckAvailabilityInput) ([]BatchAvailabilityResult, error) {
	if len(input.Items) == 0 || len(input.Items) > maxBatchAvailabilityItems {
		return nil, ErrInvalidRequest
	}
	results := make([]BatchAvailabilityResult, 0, len(input.Items))
	for _, item := range input.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		availability, err := s.CheckAvailability(ctx, item)
		if err != nil {
			return nil, err
		}
		results = append(results, BatchAvailabilityResult{
			HotelID: item.HotelID,
			RoomTypeID: item.RoomTypeID,
			AvailableQuantity: availability.AvailableQuantity,
			Available: availability.Available,
		})
	}
	return results, nil
}
