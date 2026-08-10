package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidBooking = errors.New("invalid booking")
	ErrInvalidPriceSnapshot = errors.New("invalid price snapshot")
	ErrInvalidTransition = errors.New("invalid state transition")
)

type BookingStatus string

const (
	BookingPending BookingStatus = "PENDING"
	BookingInventoryReserved BookingStatus = "INVENTORY_RESERVED"
	BookingPaymentProcessing BookingStatus = "PAYMENT_PROCESSING"
	BookingPaymentUnknown BookingStatus = "PAYMENT_UNKNOWN"
	BookingPaymentFailed BookingStatus = "PAYMENT_FAILED"
	BookingConfirmed BookingStatus = "CONFIRMED"
	BookingCancelled BookingStatus = "CANCELLED"
	BookingFailed BookingStatus = "FAILED"
)

type SagaState string

const (
	SagaPriceAccepted SagaState = "PRICE_ACCEPTED"
	SagaReservingInventory SagaState = "RESERVING_INVENTORY"
	SagaInventoryReserved SagaState = "INVENTORY_RESERVED"
	SagaPaymentProcessing SagaState = "PAYMENT_PROCESSING"
	SagaPaymentUnknown SagaState = "PAYMENT_UNKNOWN"
	SagaConfirmingReservation SagaState = "CONFIRMING_RESERVATION"
	SagaCompleted SagaState = "COMPLETED"
	SagaCompensating SagaState = "COMPENSATING"
	SagaCompensated SagaState = "COMPENSATED"
	SagaFailed SagaState = "FAILED"
)

type Booking struct {
	ID, UserID, HotelID, RoomTypeID string
	CheckIn, CheckOut time.Time
	GuestCount, RoomQuantity int
	Status BookingStatus
	ReservationID, PaymentID string
	Version int64
	CreatedAt, UpdatedAt time.Time
}

func (b Booking) Validate() error {
	if b.ID == "" || b.UserID == "" || b.HotelID == "" || b.RoomTypeID == "" {
		return fmt.Errorf("%w: required identifier is empty", ErrInvalidBooking)
	}
	if !b.CheckOut.After(b.CheckIn) || b.GuestCount < 1 || b.RoomQuantity < 1 {
		return fmt.Errorf("%w: invalid stay or party size", ErrInvalidBooking)
	}
	if b.Status == "" { return fmt.Errorf("%w: status is empty", ErrInvalidBooking) }
	return nil
}

type PriceSnapshot struct {
	BookingID, QuoteID, Currency string
	SubtotalMinor, TaxMinor, ServiceFeeMinor, DiscountMinor, TotalMinor int64
	PricingVersion string
	QuotedAt, QuoteExpiresAt, AcceptedAt time.Time
}

func (p PriceSnapshot) Validate() error {
	if p.BookingID == "" || p.QuoteID == "" || len(p.Currency) != 3 || p.PricingVersion == "" {
		return fmt.Errorf("%w: missing identity, currency, or pricing version", ErrInvalidPriceSnapshot)
	}
	if p.SubtotalMinor < 0 || p.TaxMinor < 0 || p.ServiceFeeMinor < 0 ||
		p.DiscountMinor < 0 || p.TotalMinor < 0 {
		return fmt.Errorf("%w: monetary values must use non-negative minor units", ErrInvalidPriceSnapshot)
	}
	if p.TotalMinor != p.SubtotalMinor+p.TaxMinor+p.ServiceFeeMinor-p.DiscountMinor {
		return fmt.Errorf("%w: total does not match immutable components", ErrInvalidPriceSnapshot)
	}
	if !p.QuoteExpiresAt.After(p.QuotedAt) || p.AcceptedAt.After(p.QuoteExpiresAt) {
		return fmt.Errorf("%w: quote was accepted outside its validity window", ErrInvalidPriceSnapshot)
	}
	return nil
}

type BookingSaga struct {
	ID, BookingID string
	State SagaState
	ReservationID, PaymentID string
	LastErrorCode, LastErrorMessage string
	RetryCount int
	NextRetryAt *time.Time
	Version int64
	CreatedAt, UpdatedAt time.Time
}

func (s *BookingSaga) Advance(next SagaState) error {
	allowed := map[SagaState]map[SagaState]bool{
		SagaPriceAccepted: {SagaReservingInventory:true},
		SagaReservingInventory: {SagaInventoryReserved:true, SagaFailed:true},
		SagaInventoryReserved: {SagaPaymentProcessing:true, SagaCompensating:true},
		SagaPaymentProcessing: {SagaPaymentUnknown:true, SagaConfirmingReservation:true, SagaCompensating:true},
		SagaPaymentUnknown: {SagaPaymentProcessing:true, SagaConfirmingReservation:true, SagaCompensating:true},
		SagaConfirmingReservation: {SagaCompleted:true, SagaCompensating:true},
		SagaCompensating: {SagaCompensated:true, SagaFailed:true},
	}
	if !allowed[s.State][next] { return fmt.Errorf("%w: saga %s -> %s", ErrInvalidTransition, s.State, next) }
	s.State = next
	return nil
}

type IdempotencyStatus string
const (
	IdempotencyProcessing IdempotencyStatus = "PROCESSING"
	IdempotencyCompleted IdempotencyStatus = "COMPLETED"
	IdempotencyFailed IdempotencyStatus = "FAILED"
)
type IdempotencyRecord struct {
	ID, Key, RequestHash, BookingID string
	Status IdempotencyStatus
	ResponsePayload []byte
	CreatedAt, UpdatedAt, ExpiresAt time.Time
}

type OutboxStatus string
const (
	OutboxPending OutboxStatus = "PENDING"
	OutboxPublished OutboxStatus = "PUBLISHED"
	OutboxFailed OutboxStatus = "FAILED"
)
type OutboxEvent struct {
	ID, AggregateType, AggregateID, EventType string
	EventVersion int
	Payload []byte
	Status OutboxStatus
	RetryCount int
	NextRetryAt *time.Time
	CreatedAt time.Time
	PublishedAt *time.Time
}
