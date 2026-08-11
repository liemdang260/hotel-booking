package repository

import (
	"context"
	"time"

	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
)

type CatalogSearchInput struct {
	City string
	GuestCount int32
	PageSize int32
	PageToken string
}

type Catalog interface {
	SearchCandidates(context.Context, CatalogSearchInput) (domain.CatalogSearchResult, error)
}

type AvailabilityItem struct {
	HotelID string
	RoomTypeID string
	CheckIn time.Time
	CheckOut time.Time
	Quantity int32
}

type Availability interface {
	BatchCheck(context.Context, []AvailabilityItem) ([]domain.Availability, error)
}

type PricingItem struct {
	HotelID string
	RoomTypeID string
	CheckIn time.Time
	CheckOut time.Time
	GuestCount int32
	RoomQuantity int32
}

type Pricing interface {
	BatchEstimate(context.Context, []PricingItem) ([]domain.PriceEstimate, error)
}
