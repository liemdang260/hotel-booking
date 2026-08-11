package usecase

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain/repository"
)

const (
	maxSearchGuestCount   = 16
	maxSearchRoomQuantity = 16
)

type SearchConfig struct {
	DefaultPageSize  int32
	MaxCandidates    int32
	MaxBatchItems    int
	DownstreamTimeout time.Duration
}

type SearchHotels struct {
	catalog      repository.Catalog
	availability repository.Availability
	pricing      repository.Pricing
	config       SearchConfig
}

func NewSearchHotels(catalog repository.Catalog, availability repository.Availability, pricing repository.Pricing, config SearchConfig) (*SearchHotels, error) {
	if catalog == nil || availability == nil || pricing == nil ||
		config.DefaultPageSize <= 0 || config.MaxCandidates < config.DefaultPageSize ||
		config.MaxCandidates > 50 || config.MaxBatchItems <= 0 || config.MaxBatchItems > 100 ||
		config.DownstreamTimeout <= 0 {
		return nil, domain.ErrInvalidSearch
	}
	return &SearchHotels{catalog: catalog, availability: availability, pricing: pricing, config: config}, nil
}

func (u *SearchHotels) Execute(ctx context.Context, input domain.SearchInput) (domain.SearchResult, error) {
	input.City = strings.TrimSpace(input.City)
	input.PageToken = strings.TrimSpace(input.PageToken)
	if input.City == "" || input.GuestCount <= 0 || input.GuestCount > maxSearchGuestCount ||
		input.RoomQuantity <= 0 || input.RoomQuantity > maxSearchRoomQuantity ||
		!isUTCDate(input.CheckIn) || !isUTCDate(input.CheckOut) ||
		!input.CheckOut.After(input.CheckIn) ||
		input.CheckOut.Sub(input.CheckIn) > 31*24*time.Hour {
		return domain.SearchResult{}, domain.ErrInvalidSearch
	}
	if input.PageSize == 0 {
		input.PageSize = u.config.DefaultPageSize
	}
	if input.PageSize < 1 || input.PageSize > u.config.MaxCandidates {
		return domain.SearchResult{}, domain.ErrInvalidSearch
	}

	callCtx, cancel := context.WithTimeout(ctx, u.config.DownstreamTimeout)
	defer cancel()

	catalogResult, err := u.catalog.SearchCandidates(callCtx, repository.CatalogSearchInput{
		City:       input.City,
		GuestCount: input.GuestCount,
		PageSize:   input.PageSize,
		PageToken:  input.PageToken,
	})
	if err != nil {
		return domain.SearchResult{}, err
	}
	if len(catalogResult.Candidates) > int(input.PageSize) {
		return domain.SearchResult{}, domain.ErrDependencyIncomplete
	}

	availabilityItems := make([]repository.AvailabilityItem, 0)
	pricingItems := make([]repository.PricingItem, 0)
	expected := make(map[searchResultKey]struct{})
	for _, candidate := range catalogResult.Candidates {
		for _, room := range candidate.RoomTypes {
			key := searchKey(candidate.Hotel.ID, room.ID)
			if candidate.Hotel.ID == "" || room.ID == "" || room.HotelID != candidate.Hotel.ID {
				return domain.SearchResult{}, domain.ErrDependencyIncomplete
			}
			if _, exists := expected[key]; exists {
				return domain.SearchResult{}, domain.ErrDependencyIncomplete
			}
			expected[key] = struct{}{}
			availabilityItems = append(availabilityItems, repository.AvailabilityItem{
				HotelID:     candidate.Hotel.ID,
				RoomTypeID:  room.ID,
				CheckIn:     input.CheckIn,
				CheckOut:    input.CheckOut,
				Quantity:    input.RoomQuantity,
			})
			pricingItems = append(pricingItems, repository.PricingItem{
				HotelID:      candidate.Hotel.ID,
				RoomTypeID:   room.ID,
				CheckIn:      input.CheckIn,
				CheckOut:     input.CheckOut,
				GuestCount:   input.GuestCount,
				RoomQuantity: input.RoomQuantity,
			})
		}
	}
	if len(expected) == 0 {
		return domain.SearchResult{Hotels: []domain.SearchHotel{}, NextPageToken: catalogResult.NextPageToken, Advisory: true}, nil
	}
	if len(expected) > u.config.MaxBatchItems {
		return domain.SearchResult{}, domain.ErrInvalidSearch
	}

	var availabilityResults []domain.Availability
	var pricingResults []domain.PriceEstimate
	var availabilityErr, pricingErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		availabilityResults, availabilityErr = u.availability.BatchCheck(callCtx, availabilityItems)
	}()
	go func() {
		defer wait.Done()
		pricingResults, pricingErr = u.pricing.BatchEstimate(callCtx, pricingItems)
	}()
	wait.Wait()
	if availabilityErr != nil {
		return domain.SearchResult{}, availabilityErr
	}
	if pricingErr != nil {
		return domain.SearchResult{}, pricingErr
	}

	availabilityByKey := make(map[searchResultKey]domain.Availability, len(availabilityResults))
	for _, item := range availabilityResults {
		key := searchKey(item.HotelID, item.RoomTypeID)
		if _, wanted := expected[key]; !wanted {
			return domain.SearchResult{}, domain.ErrDependencyIncomplete
		}
		if _, duplicate := availabilityByKey[key]; duplicate {
			return domain.SearchResult{}, domain.ErrDependencyIncomplete
		}
		availabilityByKey[key] = item
	}
	pricingByKey := make(map[searchResultKey]domain.PriceEstimate, len(pricingResults))
	for _, item := range pricingResults {
		key := searchKey(item.HotelID, item.RoomTypeID)
		if _, wanted := expected[key]; !wanted || item.Currency == "" || item.PricingVersion == "" || item.TotalMinor < 0 {
			return domain.SearchResult{}, domain.ErrDependencyIncomplete
		}
		if _, duplicate := pricingByKey[key]; duplicate {
			return domain.SearchResult{}, domain.ErrDependencyIncomplete
		}
		pricingByKey[key] = item
	}
	if len(availabilityByKey) != len(expected) || len(pricingByKey) != len(expected) {
		return domain.SearchResult{}, domain.ErrDependencyIncomplete
	}

	result := domain.SearchResult{
		Hotels:        make([]domain.SearchHotel, 0, len(catalogResult.Candidates)),
		NextPageToken: catalogResult.NextPageToken,
		Advisory:      true,
	}
	for _, candidate := range catalogResult.Candidates {
		hotel := domain.SearchHotel{Hotel: candidate.Hotel, Rooms: make([]domain.SearchRoom, 0, len(candidate.RoomTypes))}
		for _, room := range candidate.RoomTypes {
			key := searchKey(candidate.Hotel.ID, room.ID)
			hotel.Rooms = append(hotel.Rooms, domain.SearchRoom{
				RoomType:             room,
				AdvisoryAvailability: availabilityByKey[key],
				EstimatedPrice:        pricingByKey[key],
			})
		}
		result.Hotels = append(result.Hotels, hotel)
	}
	return result, nil
}

type searchResultKey struct {
	hotelID    string
	roomTypeID string
}

func searchKey(hotelID, roomTypeID string) searchResultKey {
	return searchResultKey{hotelID: hotelID, roomTypeID: roomTypeID}
}

func isUTCDate(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	utc := value.UTC()
	return value.Equal(time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC))
}
