package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidPayment = errors.New("invalid payment")
	ErrInvalidTransition = errors.New("invalid payment state transition")
)

type Status string

const (
	StatusPending Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed Status = "FAILED"
	StatusUnknown Status = "UNKNOWN"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusProcessing, StatusSucceeded, StatusFailed, StatusUnknown:
		return true
	default:
		return false
	}
}

type Payment struct {
	ID string
	BookingID string
	IdempotencyKey string
	AmountMinor int64
	Currency string
	PaymentMethodRef string
	Status Status
	ProviderReference string
	FailureCode string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewPayment(id, bookingID, key string, amountMinor int64, currency, methodRef string, now time.Time) (Payment, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(bookingID) == "" ||
		strings.TrimSpace(key) == "" || amountMinor <= 0 ||
		len(strings.TrimSpace(currency)) != 3 || strings.TrimSpace(methodRef) == "" ||
		now.IsZero() {
		return Payment{}, ErrInvalidPayment
	}
	return Payment{
		ID: id, BookingID: bookingID, IdempotencyKey: key,
		AmountMinor: amountMinor, Currency: strings.ToUpper(currency),
		PaymentMethodRef: methodRef, Status: StatusPending,
		CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}, nil
}

func (p Payment) SameIdentity(bookingID string, amountMinor int64, currency, methodRef string) bool {
	return p.BookingID == bookingID && p.AmountMinor == amountMinor &&
		p.Currency == strings.ToUpper(currency) && p.PaymentMethodRef == methodRef
}

type AttemptOutcome string

const (
	AttemptStarted AttemptOutcome = "STARTED"
	AttemptSucceeded AttemptOutcome = "SUCCEEDED"
	AttemptDeclined AttemptOutcome = "DECLINED"
	AttemptUnknown AttemptOutcome = "UNKNOWN"
)

type Attempt struct {
	ID string
	PaymentID string
	IdempotencyKey string
	ProviderRequestRef string
	ProviderReference string
	Outcome AttemptOutcome
	FailureCode string
	RawOutcome string
	StartedAt time.Time
	FinishedAt *time.Time
}
