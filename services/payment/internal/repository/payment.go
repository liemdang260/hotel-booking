package repository

import (
	"context"
	"errors"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
)

var (
	ErrPaymentNotFound = errors.New("payment not found")
	ErrIdempotencyConflict = errors.New("payment idempotency conflict")
	ErrBookingConflict = errors.New("booking already has another payment")
	ErrConcurrentUpdate = errors.New("payment changed concurrently")
)

type PaymentRepository interface {
	Create(context.Context, domain.Payment) (domain.Payment, error)
	GetByID(context.Context, string) (domain.Payment, error)
	GetByIdempotencyKey(context.Context, string) (domain.Payment, error)
	GetByBookingID(context.Context, string) (domain.Payment, error)

	// BeginAttempt atomically appends the audit attempt and moves PENDING/UNKNOWN to PROCESSING.
	BeginAttempt(context.Context, string, domain.Attempt, time.Time) (domain.Payment, error)
	// CompleteAttempt atomically records provider truth and updates the logical payment.
	CompleteAttempt(
		context.Context,
		string,
		string,
		domain.AttemptOutcome,
		domain.Status,
		string,
		string,
		string,
		time.Time,
	) (domain.Payment, error)
}
