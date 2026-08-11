package usecase

import (
	"context"
	"errors"
	"strings"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

var (
	ErrUnauthenticated = errors.New("booking: unauthenticated")
	ErrForbidden = errors.New("booking: forbidden")
	ErrInvalidCustomerRequest = errors.New("booking: invalid customer request")
)

type CustomerPrincipal struct {
	UserID string
	Roles []string
}

type CustomerBookingReader interface {
	FindCustomerBooking(context.Context, string) (domain.Booking, error)
	ListCustomerBookings(context.Context, string, int, string) ([]domain.Booking, string, error)
}

type CustomerBookingCreator interface {
	Create(context.Context, CreateBookingInput) (CreateBookingOutput, error)
}

type CustomerBookingCanceller interface {
	Cancel(context.Context, CancelBookingInput) (domain.BookingCancellation, error)
}

type CustomerAccess struct {
	reader CustomerBookingReader
	creator CustomerBookingCreator
	canceller CustomerBookingCanceller
}

func NewCustomerAccess(reader CustomerBookingReader, creator CustomerBookingCreator, canceller CustomerBookingCanceller) (*CustomerAccess, error) {
	if reader == nil || creator == nil || canceller == nil {
		return nil, ErrInvalidCustomerRequest
	}
	return &CustomerAccess{reader: reader, creator: creator, canceller: canceller}, nil
}

func (u *CustomerAccess) Create(ctx context.Context, actor CustomerPrincipal, bookingID, quoteID, paymentMethodID, idempotencyKey string) (CreateBookingOutput, error) {
	if !validActor(actor) || invalidValue(bookingID, 128) || invalidValue(quoteID, 128) ||
		invalidValue(paymentMethodID, 128) || invalidValue(idempotencyKey, 128) {
		return CreateBookingOutput{}, ErrInvalidCustomerRequest
	}
	return u.creator.Create(ctx, CreateBookingInput{
		BookingID: bookingID,
		UserID: actor.UserID,
		QuoteID: quoteID,
		PaymentMethodRef: paymentMethodID,
		IdempotencyKey: idempotencyKey,
	})
}

func (u *CustomerAccess) Get(ctx context.Context, actor CustomerPrincipal, bookingID string) (domain.Booking, error) {
	if !validActor(actor) || invalidValue(bookingID, 128) {
		return domain.Booking{}, ErrInvalidCustomerRequest
	}
	booking, err := u.reader.FindCustomerBooking(ctx, bookingID)
	if err != nil {
		return domain.Booking{}, err
	}
	if booking.UserID != actor.UserID {
		return domain.Booking{}, ErrForbidden
	}
	return booking, nil
}

func (u *CustomerAccess) List(ctx context.Context, actor CustomerPrincipal, pageSize int, pageToken string) ([]domain.Booking, string, error) {
	if !validActor(actor) || pageSize < 1 || pageSize > 100 || len(pageToken) > 512 {
		return nil, "", ErrInvalidCustomerRequest
	}
	return u.reader.ListCustomerBookings(ctx, actor.UserID, pageSize, pageToken)
}

func (u *CustomerAccess) Cancel(ctx context.Context, actor CustomerPrincipal, in CancelBookingInput) (domain.BookingCancellation, error) {
	if !validActor(actor) || invalidValue(in.BookingID, 128) || invalidValue(in.IdempotencyKey, 128) {
		return domain.BookingCancellation{}, ErrInvalidCustomerRequest
	}
	booking, err := u.reader.FindCustomerBooking(ctx, in.BookingID)
	if err != nil {
		return domain.BookingCancellation{}, err
	}
	if booking.UserID != actor.UserID {
		return domain.BookingCancellation{}, ErrForbidden
	}
	return u.canceller.Cancel(ctx, in)
}

func validActor(actor CustomerPrincipal) bool {
	return !invalidValue(actor.UserID, 128)
}

func invalidValue(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	return value == "" || len(value) > maximum
}
