package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidSearch = errors.New("gateway: invalid search")
	ErrDependencyIncomplete = errors.New("gateway: search dependency returned incomplete data")
)

type SearchInput struct {
	City string
	CheckIn time.Time
	CheckOut time.Time
	GuestCount int32
	RoomQuantity int32
	PageSize int32
	PageToken string
}

type Hotel struct {
	ID string
	Name string
	Description string
	Address string
	City string
	Country string
	Latitude float64
	Longitude float64
	Amenities []string
}

type RoomType struct {
	ID string
	HotelID string
	Name string
	Description string
	Capacity int32
	BedCount int32
	Amenities []string
}

type CatalogCandidate struct {
	Hotel Hotel
	RoomTypes []RoomType
}

type CatalogSearchResult struct {
	Candidates []CatalogCandidate
	NextPageToken string
}

type Availability struct {
	HotelID string
	RoomTypeID string
	Available bool
	AvailableQuantity int32
}

type PriceEstimate struct {
	HotelID string
	RoomTypeID string
	TotalMinor int64
	Currency string
	PricingVersion string
}

type SearchRoom struct {
	RoomType RoomType
	AdvisoryAvailability Availability
	EstimatedPrice PriceEstimate
}

type SearchHotel struct {
	Hotel Hotel
	Rooms []SearchRoom
}

type SearchResult struct {
	Hotels []SearchHotel
	NextPageToken string
	Advisory bool
}
