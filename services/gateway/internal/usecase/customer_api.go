package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
)

type QuoteRepository interface {
	CreateQuote(context.Context, domain.Principal, domain.CreateQuoteInput) (domain.Quote, error)
}

type BookingRepository interface {
	CreateBooking(context.Context, domain.CreateBookingInput) (domain.Booking, error)
	GetBooking(context.Context, domain.GetBookingInput) (domain.Booking, error)
	ListBookings(context.Context, domain.ListBookingsInput) (domain.BookingPage, error)
	CancelBooking(context.Context, domain.CancelBookingInput) (domain.CancellationResult, error)
}

type CustomerAPI struct {
	quotes QuoteRepository
	bookings BookingRepository
	timeout time.Duration
}

func NewCustomerAPI(quotes QuoteRepository, bookings BookingRepository, timeout time.Duration) (*CustomerAPI, error) {
	if quotes == nil || bookings == nil || timeout <= 0 {
		return nil, domain.ErrInvalidRequest
	}
	return &CustomerAPI{quotes: quotes, bookings: bookings, timeout: timeout}, nil
}

func (u *CustomerAPI) CreateQuote(ctx context.Context, actor domain.Principal, in domain.CreateQuoteInput) (domain.Quote, error) {
	if !actor.Valid() || invalidID(in.HotelID) || invalidID(in.RoomTypeID) ||
		in.GuestCount < 1 || in.GuestCount > 16 || in.RoomQuantity < 1 || in.RoomQuantity > 16 ||
		!isDate(in.CheckIn) || !isDate(in.CheckOut) || !in.CheckOut.After(in.CheckIn) ||
		in.CheckOut.Sub(in.CheckIn) > 31*24*time.Hour {
		return domain.Quote{}, domain.ErrInvalidRequest
	}
	callCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	return u.quotes.CreateQuote(callCtx, actor, in)
}

func (u *CustomerAPI) CreateBooking(ctx context.Context, in domain.CreateBookingInput) (domain.Booking, error) {
	if !in.Actor.Valid() || invalidID(in.QuoteID) || invalidID(in.PaymentMethodID) || invalidKey(in.IdempotencyKey) {
		return domain.Booking{}, domain.ErrInvalidRequest
	}
	callCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	return u.bookings.CreateBooking(callCtx, in)
}

func (u *CustomerAPI) GetBooking(ctx context.Context, in domain.GetBookingInput) (domain.Booking, error) {
	if !in.Actor.Valid() || invalidID(in.BookingID) {
		return domain.Booking{}, domain.ErrInvalidRequest
	}
	callCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	return u.bookings.GetBooking(callCtx, in)
}

func (u *CustomerAPI) ListBookings(ctx context.Context, in domain.ListBookingsInput) (domain.BookingPage, error) {
	if !in.Actor.Valid() || in.PageSize < 1 || in.PageSize > 100 || len(in.PageToken) > 512 {
		return domain.BookingPage{}, domain.ErrInvalidRequest
	}
	callCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	return u.bookings.ListBookings(callCtx, in)
}

func (u *CustomerAPI) CancelBooking(ctx context.Context, in domain.CancelBookingInput) (domain.CancellationResult, error) {
	if !in.Actor.Valid() || invalidID(in.BookingID) || invalidKey(in.IdempotencyKey) ||
		len(strings.TrimSpace(in.Reason)) == 0 || len(in.Reason) > 256 {
		return domain.CancellationResult{}, domain.ErrInvalidRequest
	}
	callCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	return u.bookings.CancelBooking(callCtx, in)
}

func invalidID(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || len(value) > 128
}

func invalidKey(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || len(value) > 128
}

func isDate(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	utc := value.UTC()
	return value.Equal(time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC))
}
