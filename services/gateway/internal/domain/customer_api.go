package domain

import (
	"errors"
	"time"
)

var (
	ErrUnauthenticated = errors.New("gateway: unauthenticated")
	ErrInvalidRequest  = errors.New("gateway: invalid request")
	ErrForbidden       = errors.New("gateway: forbidden")
	ErrNotFound        = errors.New("gateway: not found")
	ErrConflict        = errors.New("gateway: conflict")
	ErrUnavailable     = errors.New("gateway: unavailable")
)

type SubjectType string

const SubjectUser SubjectType = "USER"

type Principal struct {
	UserID      string
	Roles       []string
	SubjectType SubjectType
}

func (p Principal) Valid() bool {
	return p.UserID != "" && p.SubjectType == SubjectUser
}

type CreateQuoteInput struct {
	HotelID, RoomTypeID string
	CheckIn, CheckOut   time.Time
	GuestCount          int32
	RoomQuantity        int32
}

type Quote struct {
	ID, Currency string
	SubtotalMinor, TaxMinor, ServiceFeeMinor, DiscountMinor, TotalMinor int64
	ExpiresAt time.Time
}

type CreateBookingInput struct {
	Actor Principal
	QuoteID, PaymentMethodID, IdempotencyKey string
}

type GetBookingInput struct {
	Actor Principal
	BookingID string
}

type ListBookingsInput struct {
	Actor Principal
	PageSize int32
	PageToken string
}

type CancelBookingInput struct {
	Actor Principal
	BookingID, IdempotencyKey, Reason string
}

type Booking struct {
	ID, HotelID, RoomTypeID, Status, Currency string
	CheckIn, CheckOut, CreatedAt time.Time
	GuestCount, RoomQuantity int32
	TotalMinor int64
}

type BookingPage struct {
	Bookings []Booking
	NextPageToken string
}

type CancellationResult struct {
	Booking Booking
	CancellationID, State string
}
