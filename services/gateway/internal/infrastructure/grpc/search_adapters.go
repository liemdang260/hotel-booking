package grpc

import (
	"context"
	"time"

	catalogv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/catalog/v1"
	availabilityv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/availability/v1"
	pricingv1 "github.com/liemdang260/hotel-booking/gen/go/pricing/v1"
	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain/repository"
)

type CatalogAdapter struct {
	client catalogv1.CatalogServiceClient
}

func NewCatalogAdapter(client catalogv1.CatalogServiceClient) *CatalogAdapter {
	return &CatalogAdapter{client: client}
}

func (a *CatalogAdapter) SearchCandidates(ctx context.Context, input repository.CatalogSearchInput) (domain.CatalogSearchResult, error) {
	response, err := a.client.SearchCandidates(ctx, &catalogv1.SearchCandidatesRequest{
		City: input.City,
		GuestCount: input.GuestCount,
		PageSize: input.PageSize,
		PageToken: input.PageToken,
	})
	if err != nil {
		return domain.CatalogSearchResult{}, err
	}
	result := domain.CatalogSearchResult{
		Candidates: make([]domain.CatalogCandidate, 0, len(response.GetCandidates())),
		NextPageToken: response.GetNextPageToken(),
	}
	for _, candidate := range response.GetCandidates() {
		hotelValue := candidate.GetHotel()
		hotel := domain.Hotel{
			ID: hotelValue.GetId(),
			Name: hotelValue.GetName(),
			Description: hotelValue.GetDescription(),
			Address: hotelValue.GetAddress(),
			City: hotelValue.GetCity(),
			Country: hotelValue.GetCountry(),
			Latitude: hotelValue.GetLatitude(),
			Longitude: hotelValue.GetLongitude(),
			Amenities: append([]string(nil), hotelValue.GetAmenities()...),
		}
		item := domain.CatalogCandidate{Hotel: hotel, RoomTypes: make([]domain.RoomType, 0, len(candidate.GetRoomTypes()))}
		for _, room := range candidate.GetRoomTypes() {
			item.RoomTypes = append(item.RoomTypes, domain.RoomType{
				ID: room.GetId(),
				HotelID: room.GetHotelId(),
				Name: room.GetName(),
				Description: room.GetDescription(),
				Capacity: room.GetCapacity(),
				BedCount: room.GetBedCount(),
				Amenities: append([]string(nil), room.GetAmenities()...),
			})
		}
		result.Candidates = append(result.Candidates, item)
	}
	return result, nil
}

type AvailabilityAdapter struct {
	client availabilityv1.AvailabilityServiceClient
}

func NewAvailabilityAdapter(client availabilityv1.AvailabilityServiceClient) *AvailabilityAdapter {
	return &AvailabilityAdapter{client: client}
}

func (a *AvailabilityAdapter) BatchCheck(ctx context.Context, items []repository.AvailabilityItem) ([]domain.Availability, error) {
	request := &availabilityv1.BatchCheckAvailabilityRequest{Items: make([]*availabilityv1.BatchAvailabilityItem, 0, len(items))}
	for _, item := range items {
		request.Items = append(request.Items, &availabilityv1.BatchAvailabilityItem{
			HotelId: item.HotelID,
			RoomTypeId: item.RoomTypeID,
			CheckIn: formatDate(item.CheckIn),
			CheckOut: formatDate(item.CheckOut),
			Quantity: item.Quantity,
		})
	}
	response, err := a.client.BatchCheckAvailability(ctx, request)
	if err != nil {
		return nil, err
	}
	results := make([]domain.Availability, 0, len(response.GetResults()))
	for _, item := range response.GetResults() {
		results = append(results, domain.Availability{
			HotelID: item.GetHotelId(),
			RoomTypeID: item.GetRoomTypeId(),
			Available: item.GetAvailable(),
			AvailableQuantity: item.GetAvailableQuantity(),
		})
	}
	return results, nil
}

type PricingAdapter struct {
	client pricingv1.PricingServiceClient
}

func NewPricingAdapter(client pricingv1.PricingServiceClient) *PricingAdapter {
	return &PricingAdapter{client: client}
}

func (a *PricingAdapter) BatchEstimate(ctx context.Context, items []repository.PricingItem) ([]domain.PriceEstimate, error) {
	request := &pricingv1.BatchEstimateRequest{Items: make([]*pricingv1.EstimateItem, 0, len(items))}
	for _, item := range items {
		request.Items = append(request.Items, &pricingv1.EstimateItem{
			HotelId: item.HotelID,
			RoomTypeId: item.RoomTypeID,
			CheckIn: pricingDate(item.CheckIn),
			CheckOut: pricingDate(item.CheckOut),
			GuestCount: item.GuestCount,
			RoomQuantity: item.RoomQuantity,
		})
	}
	response, err := a.client.BatchEstimate(ctx, request)
	if err != nil {
		return nil, err
	}
	results := make([]domain.PriceEstimate, 0, len(response.GetEstimates()))
	for _, item := range response.GetEstimates() {
		results = append(results, domain.PriceEstimate{
			HotelID: item.GetHotelId(),
			RoomTypeID: item.GetRoomTypeId(),
			TotalMinor: item.GetTotalMinor(),
			Currency: item.GetCurrency(),
			PricingVersion: item.GetPricingVersion(),
		})
	}
	return results, nil
}

func formatDate(value time.Time) string {
	return value.UTC().Format(time.DateOnly)
}

func pricingDate(value time.Time) *pricingv1.Date {
	value = value.UTC()
	return &pricingv1.Date{Year: int32(value.Year()), Month: int32(value.Month()), Day: int32(value.Day())}
}
