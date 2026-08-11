package grpc

import (
	"context"
	"errors"
	"time"

	bookingv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/booking/v1"
	pricingv1 "github.com/liemdang260/hotel-booking/gen/go/pricing/v1"
	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const gatewayServiceIdentity = "gateway"

type CustomerQuoteAdapter struct {
	client pricingv1.PricingServiceClient
}

func NewCustomerQuoteAdapter(client pricingv1.PricingServiceClient) *CustomerQuoteAdapter {
	return &CustomerQuoteAdapter{client: client}
}

func (a *CustomerQuoteAdapter) CreateQuote(ctx context.Context, _ domain.Principal, input domain.CreateQuoteInput) (domain.Quote, error) {
	response, err := a.client.Quote(ctx, &pricingv1.QuoteRequest{
		HotelId: input.HotelID,
		RoomTypeId: input.RoomTypeID,
		CheckIn: pricingDate(input.CheckIn),
		CheckOut: pricingDate(input.CheckOut),
		GuestCount: input.GuestCount,
		RoomQuantity: input.RoomQuantity,
	})
	if err != nil {
		return domain.Quote{}, mapCustomerError(err)
	}
	quote := response.GetQuote()
	if quote == nil {
		return domain.Quote{}, domain.ErrUnavailable
	}
	return domain.Quote{
		ID: quote.GetQuoteId(),
		Currency: quote.GetCurrency(),
		SubtotalMinor: quote.GetSubtotalMinor(),
		TaxMinor: quote.GetTaxMinor(),
		ServiceFeeMinor: quote.GetServiceFeeMinor(),
		DiscountMinor: quote.GetDiscountMinor(),
		TotalMinor: quote.GetTotalMinor(),
		ExpiresAt: time.UnixMilli(quote.GetExpiresAtUnixMs()).UTC(),
	}, nil
}

type CustomerBookingAdapter struct {
	client bookingv1.BookingServiceClient
}

func NewCustomerBookingAdapter(client bookingv1.BookingServiceClient) *CustomerBookingAdapter {
	return &CustomerBookingAdapter{client: client}
}

func (a *CustomerBookingAdapter) CreateBooking(ctx context.Context, input domain.CreateBookingInput) (domain.Booking, error) {
	response, err := a.client.CreateBooking(ctx, &bookingv1.CreateBookingRequest{
		Actor: mapPrincipal(input.Actor),
		CallingService: gatewayServiceIdentity,
		QuoteId: input.QuoteID,
		PaymentMethodId: input.PaymentMethodID,
		IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		return domain.Booking{}, mapCustomerError(err)
	}
	return mapCustomerBooking(response.GetBooking())
}

func (a *CustomerBookingAdapter) GetBooking(ctx context.Context, input domain.GetBookingInput) (domain.Booking, error) {
	response, err := a.client.GetBooking(ctx, &bookingv1.GetBookingRequest{
		Actor: mapPrincipal(input.Actor),
		CallingService: gatewayServiceIdentity,
		BookingId: input.BookingID,
	})
	if err != nil {
		return domain.Booking{}, mapCustomerError(err)
	}
	return mapCustomerBooking(response.GetBooking())
}

func (a *CustomerBookingAdapter) ListBookings(ctx context.Context, input domain.ListBookingsInput) (domain.BookingPage, error) {
	response, err := a.client.ListBookings(ctx, &bookingv1.ListBookingsRequest{
		Actor: mapPrincipal(input.Actor),
		CallingService: gatewayServiceIdentity,
		PageSize: input.PageSize,
		PageToken: input.PageToken,
	})
	if err != nil {
		return domain.BookingPage{}, mapCustomerError(err)
	}
	result := domain.BookingPage{
		Bookings: make([]domain.Booking, 0, len(response.GetBookings())),
		NextPageToken: response.GetNextPageToken(),
	}
	for _, item := range response.GetBookings() {
		booking, mapErr := mapCustomerBooking(item)
		if mapErr != nil {
			return domain.BookingPage{}, mapErr
		}
		result.Bookings = append(result.Bookings, booking)
	}
	return result, nil
}

func (a *CustomerBookingAdapter) CancelBooking(ctx context.Context, input domain.CancelBookingInput) (domain.CancellationResult, error) {
	response, err := a.client.CancelBooking(ctx, &bookingv1.CancelBookingRequest{
		Actor: mapPrincipal(input.Actor),
		CallingService: gatewayServiceIdentity,
		BookingId: input.BookingID,
		IdempotencyKey: input.IdempotencyKey,
		Reason: input.Reason,
	})
	if err != nil {
		return domain.CancellationResult{}, mapCustomerError(err)
	}
	booking, err := mapCustomerBooking(response.GetBooking())
	if err != nil {
		return domain.CancellationResult{}, err
	}
	return domain.CancellationResult{
		Booking: booking,
		CancellationID: response.GetCancellationId(),
		State: response.GetCancellationState(),
	}, nil
}

func mapPrincipal(value domain.Principal) *bookingv1.Principal {
	return &bookingv1.Principal{
		UserId: value.UserID,
		Roles: append([]string(nil), value.Roles...),
		SubjectType: string(value.SubjectType),
	}
}

func mapCustomerBooking(value *bookingv1.Booking) (domain.Booking, error) {
	if value == nil {
		return domain.Booking{}, domain.ErrUnavailable
	}
	checkIn, err := time.Parse(time.DateOnly, value.GetCheckIn())
	if err != nil {
		return domain.Booking{}, domain.ErrUnavailable
	}
	checkOut, err := time.Parse(time.DateOnly, value.GetCheckOut())
	if err != nil {
		return domain.Booking{}, domain.ErrUnavailable
	}
	createdAt, err := time.Parse(time.RFC3339Nano, value.GetCreatedAt())
	if err != nil {
		return domain.Booking{}, domain.ErrUnavailable
	}
	return domain.Booking{
		ID: value.GetId(),
		HotelID: value.GetHotelId(),
		RoomTypeID: value.GetRoomTypeId(),
		CheckIn: checkIn.UTC(),
		CheckOut: checkOut.UTC(),
		GuestCount: value.GetGuestCount(),
		RoomQuantity: value.GetRoomQuantity(),
		Status: value.GetStatus(),
		TotalMinor: value.GetTotalMinor(),
		Currency: value.GetCurrency(),
		CreatedAt: createdAt.UTC(),
	}, nil
}

func mapCustomerError(err error) error {
	switch status.Code(err) {
	case codes.Canceled:
		return context.Canceled
	case codes.DeadlineExceeded:
		return context.DeadlineExceeded
	case codes.InvalidArgument:
		return domain.ErrInvalidRequest
	case codes.Unauthenticated:
		return domain.ErrUnauthenticated
	case codes.PermissionDenied:
		return domain.ErrForbidden
	case codes.NotFound:
		return domain.ErrNotFound
	case codes.AlreadyExists, codes.FailedPrecondition, codes.Aborted:
		return domain.ErrConflict
	case codes.Unavailable, codes.ResourceExhausted:
		return domain.ErrUnavailable
	default:
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return domain.ErrUnavailable
	}
}
