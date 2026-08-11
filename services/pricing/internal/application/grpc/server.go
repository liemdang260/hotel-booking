package grpc

import (
	"context"
	"errors"
	"time"

	pricingv1 "github.com/liemdang260/hotel-booking/gen/go/pricing/v1"
	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
	"github.com/liemdang260/hotel-booking/services/pricing/internal/repository"
	"github.com/liemdang260/hotel-booking/services/pricing/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type quoteCreator interface {
	Execute(context.Context, usecase.CreateQuoteInput) (domain.Quote, error)
}
type quoteGetter interface {
	Execute(context.Context, string) (domain.Quote, error)
}
type batchEstimator interface {
	Execute(context.Context, []usecase.EstimateInput) ([]usecase.Estimate, error)
}

type Server struct {
	create quoteCreator
	get quoteGetter
	estimate batchEstimator
}

func NewServer(create quoteCreator, get quoteGetter, estimate batchEstimator) *Server {
	return &Server{create: create, get: get, estimate: estimate}
}

func (s *Server) Quote(ctx context.Context, request *pricingv1.QuoteRequest) (*pricingv1.QuoteResponse, error) {
	input, err := quoteInput(request.GetHotelId(), request.GetRoomTypeId(), request.GetCheckIn(), request.GetCheckOut(), request.GetGuestCount(), request.GetRoomQuantity())
	if err != nil {
		return nil, mapError(err)
	}
	quote, err := s.create.Execute(ctx, input)
	if err != nil {
		return nil, mapError(err)
	}
	return &pricingv1.QuoteResponse{Quote: mapQuote(quote)}, nil
}

func (s *Server) GetQuote(ctx context.Context, request *pricingv1.GetQuoteRequest) (*pricingv1.GetQuoteResponse, error) {
	quote, err := s.get.Execute(ctx, request.GetQuoteId())
	if err != nil {
		return nil, mapError(err)
	}
	return &pricingv1.GetQuoteResponse{Quote: mapQuote(quote)}, nil
}

func (s *Server) BatchEstimate(ctx context.Context, request *pricingv1.BatchEstimateRequest) (*pricingv1.BatchEstimateResponse, error) {
	items := request.GetItems()
	inputs := make([]usecase.EstimateInput, 0, len(items))
	for _, item := range items {
		input, err := quoteInput(item.GetHotelId(), item.GetRoomTypeId(), item.GetCheckIn(), item.GetCheckOut(), item.GetGuestCount(), item.GetRoomQuantity())
		if err != nil {
			return nil, mapError(err)
		}
		inputs = append(inputs, usecase.EstimateInput(input))
	}
	estimates, err := s.estimate.Execute(ctx, inputs)
	if err != nil {
		return nil, mapError(err)
	}
	response := &pricingv1.BatchEstimateResponse{Estimates: make([]*pricingv1.EstimateValue, 0, len(estimates))}
	for _, estimate := range estimates {
		response.Estimates = append(response.Estimates, &pricingv1.EstimateValue{
			HotelId: estimate.HotelID,
			RoomTypeId: estimate.RoomTypeID,
			TotalMinor: estimate.TotalMinor,
			Currency: estimate.Currency,
			PricingVersion: estimate.PricingVersion,
		})
	}
	return response, nil
}

func quoteInput(hotelID, roomTypeID string, checkIn, checkOut *pricingv1.Date, guests, rooms int32) (usecase.CreateQuoteInput, error) {
	if checkIn == nil || checkOut == nil {
		return usecase.CreateQuoteInput{}, domain.ErrInvalidStay
	}
	in, err := domain.NewDate(int(checkIn.GetYear()), time.Month(checkIn.GetMonth()), int(checkIn.GetDay()))
	if err != nil {
		return usecase.CreateQuoteInput{}, err
	}
	out, err := domain.NewDate(int(checkOut.GetYear()), time.Month(checkOut.GetMonth()), int(checkOut.GetDay()))
	if err != nil {
		return usecase.CreateQuoteInput{}, err
	}
	return usecase.CreateQuoteInput{
		HotelID: hotelID,
		RoomTypeID: roomTypeID,
		CheckIn: in,
		CheckOut: out,
		GuestCount: int(guests),
		RoomQuantity: int(rooms),
	}, nil
}

func mapDate(value domain.Date) *pricingv1.Date {
	return &pricingv1.Date{Year: int32(value.Year), Month: int32(value.Month), Day: int32(value.Day)}
}

func mapQuote(quote domain.Quote) *pricingv1.QuoteValue {
	return &pricingv1.QuoteValue{
		QuoteId: quote.ID,
		HotelId: quote.Input.HotelID,
		RoomTypeId: quote.Input.RoomTypeID,
		CheckIn: mapDate(quote.Input.CheckIn),
		CheckOut: mapDate(quote.Input.CheckOut),
		GuestCount: int32(quote.Input.GuestCount),
		RoomQuantity: int32(quote.Input.RoomQuantity),
		SubtotalMinor: quote.Price.SubtotalMinor,
		TaxMinor: quote.Price.TaxMinor,
		ServiceFeeMinor: quote.Price.ServiceFeeMinor,
		DiscountMinor: quote.Price.DiscountMinor,
		TotalMinor: quote.Price.TotalMinor,
		Currency: quote.Currency,
		PricingVersion: quote.PricingVersion,
		CreatedAtUnixMs: quote.CreatedAt.UnixMilli(),
		ExpiresAtUnixMs: quote.ExpiresAt.UnixMilli(),
		CancellationPolicy: &pricingv1.CancellationPolicyValue{
			PolicyCode: quote.CancellationPolicy.PolicyCode,
			PolicyVersion: quote.CancellationPolicy.PolicyVersion,
			FreeCancelUntilUnixMs: quote.CancellationPolicy.FreeCancelUntil.UnixMilli(),
			RefundBasisPoints: int32(quote.CancellationPolicy.RefundBasisPoints),
			CancellationFeeMinor: quote.CancellationPolicy.CancellationFeeMinor,
		},
	}
}

func mapError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "pricing operation canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "pricing operation deadline exceeded")
	case errors.Is(err, domain.ErrInvalidStay), errors.Is(err, domain.ErrInvalidParty), errors.Is(err, domain.ErrInvalidMoney):
		return status.Error(codes.InvalidArgument, "invalid pricing request")
	case errors.Is(err, domain.ErrQuoteExpired):
		return status.Error(codes.FailedPrecondition, "quote expired")
	case errors.Is(err, repository.ErrQuoteNotFound):
		return status.Error(codes.NotFound, "quote not found")
	default:
		return status.Error(codes.Internal, "pricing operation failed")
	}
}
