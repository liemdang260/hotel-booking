package repository

import (
	"context"
	"errors"
	"time"
)

var (
	ErrQuoteNotFound=errors.New("quote not found");ErrQuoteExpired=errors.New("quote expired");ErrQuoteMismatch=errors.New("quote does not match booking request");ErrSoldOut=errors.New("inventory sold out");ErrInventoryNotConfigured=errors.New("inventory not configured");ErrReservationNotFound=errors.New("reservation not found");ErrReservationExpired=errors.New("reservation expired");ErrIdempotencyConflict=errors.New("idempotency conflict");ErrPaymentDeclined=errors.New("payment declined");ErrPaymentNotFound=errors.New("payment not found");ErrOutcomeUnknown=errors.New("remote mutation outcome unknown");ErrDownstreamUnavailable=errors.New("downstream unavailable");ErrInvalidRemoteResponse=errors.New("invalid remote response")
)

type CancellationPolicy struct {
	PolicyCode string
	PolicyVersion string
	FreeCancelUntil time.Time
	RefundBasisPoints int
	CancellationFeeMinor int64
}

type Quote struct {
	ID,HotelID,RoomTypeID,Currency,PricingVersion string
	CheckIn,CheckOut time.Time
	GuestCount,RoomQuantity int
	SubtotalMinor,TaxMinor,ServiceFeeMinor,DiscountMinor,TotalMinor int64
	CancellationPolicy CancellationPolicy
	CreatedAt,ExpiresAt time.Time
}
type PricingRepository interface{GetQuote(context.Context,string)(Quote,error)}

type ReservationStatus string
const(ReservationHeld ReservationStatus="HELD";ReservationBooked ReservationStatus="BOOKED";ReservationReleased ReservationStatus="RELEASED";ReservationExpiredStatus ReservationStatus="EXPIRED")
type ReserveInventoryCommand struct{BookingID,HotelID,RoomTypeID string;CheckIn,CheckOut time.Time;Quantity int}
type Reservation struct{ID,BookingID string;Status ReservationStatus;ExpiresAt time.Time}
type AvailabilityRepository interface{ReserveInventory(context.Context,ReserveInventoryCommand)(Reservation,error);GetReservation(context.Context,string)(Reservation,error);ConfirmReservation(context.Context,string)(Reservation,error);ReleaseReservation(context.Context,string,string)(Reservation,error)}

type PaymentStatus string
const(PaymentPending PaymentStatus="PENDING";PaymentProcessing PaymentStatus="PROCESSING";PaymentSucceeded PaymentStatus="SUCCEEDED";PaymentFailed PaymentStatus="FAILED";PaymentUnknown PaymentStatus="UNKNOWN")
type CreatePaymentCommand struct{BookingID,IdempotencyKey,Currency,PaymentMethodRef string;AmountMinor int64}
type Payment struct{ID,BookingID string;Status PaymentStatus;ProviderReference string}
type PaymentRepository interface{CreatePayment(context.Context,CreatePaymentCommand)(Payment,error);GetPayment(context.Context,string)(Payment,error)}
