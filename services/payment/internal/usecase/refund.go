package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/provider"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

type CreateRefundInput struct {
	PaymentID      string
	BookingID      string
	IdempotencyKey string
	AmountMinor    int64
	Currency       string
}

type CreateRefund struct {
	refunds  repository.RefundRepository
	provider provider.RefundProvider
	ids      IDGenerator
	clock    Clock
}

func NewCreateRefund(r repository.RefundRepository, p provider.RefundProvider, ids IDGenerator, c Clock) *CreateRefund {
	return &CreateRefund{refunds: r, provider: p, ids: ids, clock: c}
}

func (u *CreateRefund) Execute(ctx context.Context, in CreateRefundInput) (domain.Refund, error) {
	if prior, err := u.refunds.GetByIdempotencyKey(ctx, in.IdempotencyKey); err == nil {
		if !prior.SameIdentity(in.PaymentID, in.BookingID, in.AmountMinor, in.Currency) {
			return domain.Refund{}, repository.ErrRefundIdempotencyConflict
		}
		return prior, nil
	} else if !errors.Is(err, repository.ErrRefundNotFound) {
		return domain.Refund{}, err
	}
	refund, err := domain.NewRefund(u.ids.NewID(), in.PaymentID, in.BookingID, in.IdempotencyKey, in.AmountMinor, in.Currency, u.clock.Now())
	if err != nil {
		return domain.Refund{}, err
	}
	refund, err = u.refunds.Create(ctx, refund)
	if errors.Is(err, repository.ErrRefundIdempotencyConflict) {
		return u.Execute(ctx, in)
	}
	if err != nil {
		return domain.Refund{}, err
	}
	attempt := domain.RefundAttempt{
		ID: u.ids.NewID(), RefundID: refund.ID, Outcome: domain.AttemptStarted,
		StartedAt: u.clock.Now().UTC(),
	}
	refund, err = u.refunds.BeginAttempt(ctx, refund.ID, attempt, u.clock.Now().UTC())
	if err != nil {
		return domain.Refund{}, err
	}
	result := u.provider.Refund(ctx, refundRequest(refund))
	status, err := refundStatusFor(result.Outcome)
	if err != nil {
		return domain.Refund{}, err
	}
	return u.refunds.CompleteAttempt(ctx, refund.ID, attempt.ID, result.Outcome, status,
		result.ProviderRequestRef, result.ProviderReference, result.FailureCode, result.RawOutcome, u.clock.Now().UTC())
}

type GetRefund struct {
	refunds  repository.RefundRepository
	provider provider.RefundProvider
	clock    Clock
}

func NewGetRefund(r repository.RefundRepository, p provider.RefundProvider, c Clock) *GetRefund {
	return &GetRefund{refunds: r, provider: p, clock: c}
}

func (u *GetRefund) Execute(ctx context.Context, id string) (domain.Refund, error) {
	if id == "" {
		return domain.Refund{}, domain.ErrInvalidRefund
	}
	refund, err := u.refunds.GetByID(ctx, id)
	if err != nil || refund.Status != domain.RefundUnknown {
		return refund, err
	}
	result := u.provider.GetRefund(ctx, refundRequest(refund))
	if result.Outcome == domain.AttemptUnknown {
		return refund, nil
	}
	status, err := refundStatusFor(result.Outcome)
	if err != nil {
		return domain.Refund{}, err
	}
	return u.refunds.ResolveUnknown(ctx, refund.ID, status, result.ProviderReference, result.FailureCode, u.clock.Now().UTC())
}

func refundRequest(r domain.Refund) provider.RefundRequest {
	return provider.RefundRequest{
		RefundID: r.ID, PaymentID: r.PaymentID, BookingID: r.BookingID,
		IdempotencyKey: r.IdempotencyKey, AmountMinor: r.AmountMinor, Currency: r.Currency,
	}
}

func refundStatusFor(outcome domain.AttemptOutcome) (domain.RefundStatus, error) {
	switch outcome {
	case domain.AttemptSucceeded:
		return domain.RefundSucceeded, nil
	case domain.AttemptDeclined:
		return domain.RefundFailed, nil
	case domain.AttemptUnknown:
		return domain.RefundUnknown, nil
	default:
		return "", fmt.Errorf("unsupported refund outcome %q", outcome)
	}
}
