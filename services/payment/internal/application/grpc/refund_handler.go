package grpc

import (
	"context"
	"errors"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
	"github.com/liemdang260/hotel-booking/services/payment/internal/usecase"
)

type CreateRefundUsecase interface {
	Execute(context.Context, usecase.CreateRefundInput) (domain.Refund, error)
}

type GetRefundUsecase interface {
	Execute(context.Context, string) (domain.Refund, error)
}

type RefundHandler struct {
	create CreateRefundUsecase
	get    GetRefundUsecase
}

func NewRefundHandler(create CreateRefundUsecase, get GetRefundUsecase) *RefundHandler {
	return &RefundHandler{create: create, get: get}
}

type CreateRefundRequest struct {
	PaymentID      string
	BookingID      string
	IdempotencyKey string
	AmountMinor    int64
	Currency       string
}

type GetRefundRequest struct{ RefundID string }

type RefundResponse struct {
	Refund domain.Refund
	Error  *Error
}

func (h *RefundHandler) CreateRefund(ctx context.Context, r CreateRefundRequest) RefundResponse {
	refund, err := h.create.Execute(ctx, usecase.CreateRefundInput{
		PaymentID: r.PaymentID, BookingID: r.BookingID, IdempotencyKey: r.IdempotencyKey,
		AmountMinor: r.AmountMinor, Currency: r.Currency,
	})
	return mapRefundResponse(refund, err)
}

func (h *RefundHandler) GetRefund(ctx context.Context, r GetRefundRequest) RefundResponse {
	refund, err := h.get.Execute(ctx, r.RefundID)
	return mapRefundResponse(refund, err)
}

func mapRefundResponse(refund domain.Refund, err error) RefundResponse {
	if err == nil {
		return RefundResponse{Refund: refund}
	}
	code := "INTERNAL"
	switch {
	case errors.Is(err, domain.ErrInvalidRefund):
		code = "INVALID_ARGUMENT"
	case errors.Is(err, repository.ErrRefundNotFound):
		code = "NOT_FOUND"
	case errors.Is(err, repository.ErrRefundIdempotencyConflict):
		code = "IDEMPOTENCY_CONFLICT"
	case errors.Is(err, repository.ErrConcurrentUpdate):
		code = "CONCURRENT_UPDATE"
	}
	return RefundResponse{Error: &Error{Code: code, Message: publicMessage(code)}}
}
