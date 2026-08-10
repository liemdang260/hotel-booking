package grpc

import (
	"context"
	"errors"
	"time"

	availabilityv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/availability/v1"
	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/domain/repository"
	"github.com/liemdang260/hotel-booking/services/availability/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AvailabilityUsecases interface {
	CheckAvailability(context.Context, usecase.CheckAvailabilityInput) (usecase.AvailabilityResult, error)
	ReserveInventory(context.Context, usecase.ReserveInventoryInput) (usecase.ReservationResult, error)
	ConfirmReservation(context.Context, domain.ReservationID) (usecase.ReservationResult, error)
	ReleaseReservation(context.Context, domain.ReservationID) (usecase.ReservationResult, error)
}

type Server struct {
	usecases AvailabilityUsecases
}

func NewServer(usecases AvailabilityUsecases) *Server {
	return &Server{usecases: usecases}
}

func (s *Server) CheckAvailability(ctx context.Context, request *availabilityv1.CheckAvailabilityRequest) (*availabilityv1.CheckAvailabilityResponse, error) {
	checkIn, checkOut, err := parseStay(request.GetCheckIn(), request.GetCheckOut())
	if err != nil { return nil, mapError(err) }
	result, err := s.usecases.CheckAvailability(ctx, usecase.CheckAvailabilityInput{
		HotelID:domain.HotelID(request.GetHotelId()), RoomTypeID:domain.RoomTypeID(request.GetRoomTypeId()),
		CheckIn:checkIn, CheckOut:checkOut, Quantity:int(request.GetQuantity()),
	})
	if err != nil { return nil, mapError(err) }
	return &availabilityv1.CheckAvailabilityResponse{
		Available:result.Available, AvailableQuantity:int32(result.AvailableQuantity),
	}, nil
}

func (s *Server) ReserveInventory(ctx context.Context, request *availabilityv1.ReserveInventoryRequest) (*availabilityv1.ReservationResponse, error) {
	checkIn, checkOut, err := parseStay(request.GetCheckIn(), request.GetCheckOut())
	if err != nil { return nil, mapError(err) }
	result, err := s.usecases.ReserveInventory(ctx, usecase.ReserveInventoryInput{
		BookingID:domain.BookingID(request.GetBookingId()), HotelID:domain.HotelID(request.GetHotelId()),
		RoomTypeID:domain.RoomTypeID(request.GetRoomTypeId()), CheckIn:checkIn, CheckOut:checkOut,
		Quantity:int(request.GetQuantity()), HoldTTL:time.Duration(request.GetHoldTtlSeconds())*time.Second,
	})
	if err != nil { return nil, mapError(err) }
	return reservationResponse(result), nil
}

func (s *Server) ConfirmReservation(ctx context.Context, request *availabilityv1.ConfirmReservationRequest) (*availabilityv1.ReservationResponse, error) {
	result, err := s.usecases.ConfirmReservation(ctx, domain.ReservationID(request.GetReservationId()))
	if err != nil { return nil, mapError(err) }
	return reservationResponse(result), nil
}

func (s *Server) ReleaseReservation(ctx context.Context, request *availabilityv1.ReleaseReservationRequest) (*availabilityv1.ReservationResponse, error) {
	result, err := s.usecases.ReleaseReservation(ctx, domain.ReservationID(request.GetReservationId()))
	if err != nil { return nil, mapError(err) }
	return reservationResponse(result), nil
}

func parseStay(checkInValue, checkOutValue string) (time.Time, time.Time, error) {
	checkIn, err := time.Parse(time.DateOnly, checkInValue)
	if err != nil { return time.Time{}, time.Time{}, usecase.ErrInvalidRequest }
	checkOut, err := time.Parse(time.DateOnly, checkOutValue)
	if err != nil { return time.Time{}, time.Time{}, usecase.ErrInvalidRequest }
	return checkIn.UTC(), checkOut.UTC(), nil
}

func reservationResponse(result usecase.ReservationResult) *availabilityv1.ReservationResponse {
	response := &availabilityv1.ReservationResponse{
		ReservationId:string(result.ReservationID), Status:string(result.Status),
	}
	if result.ExpiresAt != nil { response.ExpiresAt = result.ExpiresAt.UTC().Format(time.RFC3339Nano) }
	return response
}

func mapError(err error) error {
	switch {
	case errors.Is(err, usecase.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, repository.ErrNotFound):
		return status.Error(codes.NotFound, "reservation not found")
	case errors.Is(err, usecase.ErrSoldOut):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, usecase.ErrIdempotencyConflict), errors.Is(err, usecase.ErrInvalidTransition):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, repository.ErrConcurrentWrite):
		return status.Error(codes.Aborted, "concurrent inventory update")
	default:
		return status.Error(codes.Internal, "availability operation failed")
	}
}
