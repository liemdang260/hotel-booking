package domain

import (
	"errors"
	"strings"
	"time"
)

var ErrInvalidRefund = errors.New("invalid refund")

type RefundStatus string

const (
	RefundPending    RefundStatus = "PENDING"
	RefundProcessing RefundStatus = "PROCESSING"
	RefundSucceeded  RefundStatus = "SUCCEEDED"
	RefundFailed     RefundStatus = "FAILED"
	RefundUnknown    RefundStatus = "UNKNOWN"
)

type Refund struct {
	ID                string
	PaymentID         string
	BookingID         string
	IdempotencyKey    string
	AmountMinor       int64
	Currency          string
	Status            RefundStatus
	ProviderReference string
	FailureCode       string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func NewRefund(id, paymentID, bookingID, key string, amount int64, currency string, now time.Time) (Refund, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(paymentID) == "" ||
		strings.TrimSpace(bookingID) == "" || strings.TrimSpace(key) == "" ||
		amount <= 0 || len(strings.TrimSpace(currency)) != 3 || now.IsZero() {
		return Refund{}, ErrInvalidRefund
	}
	return Refund{
		ID: id, PaymentID: paymentID, BookingID: bookingID, IdempotencyKey: key,
		AmountMinor: amount, Currency: strings.ToUpper(currency), Status: RefundPending,
		CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}, nil
}

func (r Refund) SameIdentity(paymentID, bookingID string, amount int64, currency string) bool {
	return r.PaymentID == paymentID && r.BookingID == bookingID &&
		r.AmountMinor == amount && r.Currency == strings.ToUpper(currency)
}

type RefundAttempt struct {
	ID                   string
	RefundID             string
	Outcome              AttemptOutcome
	ProviderRequestRef   string
	ProviderReference    string
	FailureCode          string
	RawOutcome           string
	StartedAt            time.Time
	FinishedAt           *time.Time
}
