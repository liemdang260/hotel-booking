package repository

import (
	"context"
	"errors"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
)

var (
	ErrRefundNotFound            = errors.New("refund not found")
	ErrRefundIdempotencyConflict = errors.New("refund idempotency conflict")
	ErrRefundPaymentConflict     = errors.New("refund payment conflict")
)

type PaymentReader interface {
	GetByID(context.Context, string) (domain.Payment, error)
}

type RefundRepository interface {
	Create(context.Context, domain.Refund) (domain.Refund, error)
	GetByID(context.Context, string) (domain.Refund, error)
	GetByIdempotencyKey(context.Context, string) (domain.Refund, error)
	BeginAttempt(context.Context, string, domain.RefundAttempt, time.Time) (domain.Refund, error)
	CompleteAttempt(
		context.Context, string, string, domain.AttemptOutcome, domain.RefundStatus,
		string, string, string, string, time.Time,
	) (domain.Refund, error)
	ResolveUnknown(
		context.Context, string, domain.RefundStatus, string, string, time.Time,
	) (domain.Refund, error)
}
