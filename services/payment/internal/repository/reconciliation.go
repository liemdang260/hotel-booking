package repository

import (
	"context"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
)

type ReconciliationStatus string

const (
	ReconciliationPending ReconciliationStatus = "PENDING"
	ReconciliationClaimed ReconciliationStatus = "CLAIMED"
	ReconciliationResolved ReconciliationStatus = "RESOLVED"
	ReconciliationExhausted ReconciliationStatus = "EXHAUSTED"
)

type ReconciliationJob struct {
	PaymentID string
	IdempotencyKey string
	ProviderReference string
	Status ReconciliationStatus
	RetryCount int
	MaxAttempts int
	NextRetryAt time.Time
	LeaseUntil *time.Time
	LastErrorCode string
	Version int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ReconciliationRepository interface {
	// EnsurePending is idempotent by payment ID.
	EnsurePending(context.Context, string, time.Time, int, time.Time) error
	// ClaimDue commits its lease before the caller performs a provider request.
	ClaimDue(context.Context, time.Time, time.Time, int) ([]ReconciliationJob, error)
	Resolve(
		context.Context,
		string,
		int64,
		domain.Status,
		string,
		string,
		time.Time,
	) (domain.Payment, error)
	Reschedule(context.Context, string, int64, int, time.Time, string, time.Time) error
	Exhaust(context.Context, string, int64, int, string, time.Time) error
}
