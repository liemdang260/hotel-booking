package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/provider"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

type IDGenerator interface { NewID() string }
type Clock interface { Now() time.Time }

type CreatePaymentInput struct {
	BookingID string
	IdempotencyKey string
	AmountMinor int64
	Currency string
	PaymentMethodRef string
}

type CreatePayment struct {
	payments repository.PaymentRepository
	provider provider.PaymentProvider
	ids IDGenerator
	clock Clock
}

func NewCreatePayment(r repository.PaymentRepository, p provider.PaymentProvider, ids IDGenerator, clock Clock) *CreatePayment {
	return &CreatePayment{payments:r, provider:p, ids:ids, clock:clock}
}

func (u *CreatePayment) Execute(ctx context.Context, in CreatePaymentInput) (domain.Payment, error) {
	if prior, err := u.payments.GetByIdempotencyKey(ctx, in.IdempotencyKey); err == nil {
		if !prior.SameIdentity(in.BookingID, in.AmountMinor, in.Currency, in.PaymentMethodRef) {
			return domain.Payment{}, repository.ErrIdempotencyConflict
		}
		return prior, nil
	} else if !errors.Is(err, repository.ErrPaymentNotFound) {
		return domain.Payment{}, err
	}

	if prior, err := u.payments.GetByBookingID(ctx, in.BookingID); err == nil {
		if prior.IdempotencyKey != in.IdempotencyKey ||
			!prior.SameIdentity(in.BookingID, in.AmountMinor, in.Currency, in.PaymentMethodRef) {
			return domain.Payment{}, repository.ErrBookingConflict
		}
		return prior, nil
	} else if !errors.Is(err, repository.ErrPaymentNotFound) {
		return domain.Payment{}, err
	}

	now := u.clock.Now()
	payment, err := domain.NewPayment(
		u.ids.NewID(), in.BookingID, in.IdempotencyKey, in.AmountMinor,
		in.Currency, in.PaymentMethodRef, now,
	)
	if err != nil { return domain.Payment{}, err }
	payment, err = u.payments.Create(ctx, payment)
	if err != nil {
		if errors.Is(err, repository.ErrIdempotencyConflict) || errors.Is(err, repository.ErrBookingConflict) {
			// Resolve a racing replay through the authoritative persisted identity.
			return u.resolveReplay(ctx, in)
		}
		return domain.Payment{}, err
	}

	attempt := domain.Attempt{
		ID:u.ids.NewID(), PaymentID:payment.ID, IdempotencyKey:payment.IdempotencyKey,
		Outcome:domain.AttemptStarted, StartedAt:u.clock.Now().UTC(),
	}
	payment, err = u.payments.BeginAttempt(ctx, payment.ID, attempt, u.clock.Now().UTC())
	if err != nil { return domain.Payment{}, err }

	result := u.provider.Charge(ctx, provider.ChargeRequest{
		PaymentID:payment.ID, BookingID:payment.BookingID,
		IdempotencyKey:payment.IdempotencyKey, AmountMinor:payment.AmountMinor,
		Currency:payment.Currency, PaymentMethodRef:payment.PaymentMethodRef,
	})
	finalStatus, err := statusFor(result.Outcome)
	if err != nil { return domain.Payment{}, err }
	return u.payments.CompleteAttempt(
		ctx, payment.ID, attempt.ID, result.Outcome, finalStatus,
		result.ProviderRequestRef, result.ProviderReference,
		result.FailureCode, result.RawOutcome, u.clock.Now().UTC(),
	)
}

func (u *CreatePayment) resolveReplay(ctx context.Context, in CreatePaymentInput) (domain.Payment, error) {
	prior, err := u.payments.GetByIdempotencyKey(ctx, in.IdempotencyKey)
	if err != nil { return domain.Payment{}, err }
	if !prior.SameIdentity(in.BookingID, in.AmountMinor, in.Currency, in.PaymentMethodRef) {
		return domain.Payment{}, repository.ErrIdempotencyConflict
	}
	return prior, nil
}

func statusFor(outcome domain.AttemptOutcome) (domain.Status, error) {
	switch outcome {
	case domain.AttemptSucceeded:
		return domain.StatusSucceeded, nil
	case domain.AttemptDeclined:
		return domain.StatusFailed, nil
	case domain.AttemptUnknown:
		return domain.StatusUnknown, nil
	default:
		return "", fmt.Errorf("unsupported provider outcome %q", outcome)
	}
}

type GetPayment struct { payments repository.PaymentRepository }
func NewGetPayment(r repository.PaymentRepository) *GetPayment { return &GetPayment{payments:r} }
func (u *GetPayment) Execute(ctx context.Context, id string) (domain.Payment, error) {
	if id == "" { return domain.Payment{}, domain.ErrInvalidPayment }
	return u.payments.GetByID(ctx, id)
}
