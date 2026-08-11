package grpc

import (
	"context"
	"errors"

	availabilityv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/availability/v1"
	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type batchAvailabilityUsecases interface {
	BatchCheckAvailability(context.Context, usecase.BatchCheckAvailabilityInput) ([]usecase.BatchAvailabilityResult, error)
}

func (s *Server) BatchCheckAvailability(ctx context.Context, request *availabilityv1.BatchCheckAvailabilityRequest) (*availabilityv1.BatchCheckAvailabilityResponse, error) {
	batch, ok := s.usecases.(batchAvailabilityUsecases)
	if !ok {
		return nil, mapError(usecase.ErrInvalidRequest)
	}
	items := request.GetItems()
	input := usecase.BatchCheckAvailabilityInput{Items: make([]usecase.CheckAvailabilityInput, 0, len(items))}
	for _, item := range items {
		checkIn, checkOut, err := parseStay(item.GetCheckIn(), item.GetCheckOut())
		if err != nil {
			return nil, mapError(err)
		}
		input.Items = append(input.Items, usecase.CheckAvailabilityInput{
			HotelID:    domain.HotelID(item.GetHotelId()),
			RoomTypeID: domain.RoomTypeID(item.GetRoomTypeId()),
			CheckIn:    checkIn,
			CheckOut:   checkOut,
			Quantity:   int(item.GetQuantity()),
		})
	}
	results, err := batch.BatchCheckAvailability(ctx, input)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return nil, status.Error(codes.Canceled, "request canceled")
		case errors.Is(err, context.DeadlineExceeded):
			return nil, status.Error(codes.DeadlineExceeded, "request deadline exceeded")
		default:
			return nil, mapError(err)
		}
	}
	response := &availabilityv1.BatchCheckAvailabilityResponse{
		Results: make([]*availabilityv1.BatchAvailabilityResult, 0, len(results)),
	}
	for _, result := range results {
		response.Results = append(response.Results, &availabilityv1.BatchAvailabilityResult{
			HotelId:              string(result.HotelID),
			RoomTypeId:           string(result.RoomTypeID),
			Available:            result.Available,
			AvailableQuantity:    int32(result.AvailableQuantity),
		})
	}
	return response, nil
}
