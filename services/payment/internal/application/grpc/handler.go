package grpc

import (
	"context"
	"errors"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
	"github.com/liemdang260/hotel-booking/services/payment/internal/usecase"
)

type CreatePaymentUsecase interface {
	Execute(context.Context, usecase.CreatePaymentInput) (domain.Payment,error)
}
type GetPaymentUsecase interface {
	Execute(context.Context,string) (domain.Payment,error)
}

type Handler struct { create CreatePaymentUsecase; get GetPaymentUsecase }
func NewHandler(create CreatePaymentUsecase,get GetPaymentUsecase)*Handler{return &Handler{create:create,get:get}}

type CreateRequest struct{ BookingID,IdempotencyKey,Currency,PaymentMethodRef string; AmountMinor int64 }
type GetRequest struct{ PaymentID string }
type Response struct{ Payment domain.Payment; Error *Error }
type Error struct{ Code,Message string }

func(h *Handler)CreatePayment(ctx context.Context,r CreateRequest)Response{
	p,err:=h.create.Execute(ctx,usecase.CreatePaymentInput{
		BookingID:r.BookingID,IdempotencyKey:r.IdempotencyKey,AmountMinor:r.AmountMinor,
		Currency:r.Currency,PaymentMethodRef:r.PaymentMethodRef,
	})
	return mapResponse(p,err)
}
func(h *Handler)GetPayment(ctx context.Context,r GetRequest)Response{
	p,err:=h.get.Execute(ctx,r.PaymentID);return mapResponse(p,err)
}
func mapResponse(p domain.Payment,err error)Response{
	if err==nil{return Response{Payment:p}}
	code:="INTERNAL"
	switch {
	case errors.Is(err,domain.ErrInvalidPayment):code="INVALID_ARGUMENT"
	case errors.Is(err,repository.ErrPaymentNotFound):code="NOT_FOUND"
	case errors.Is(err,repository.ErrIdempotencyConflict):code="IDEMPOTENCY_CONFLICT"
	case errors.Is(err,repository.ErrBookingConflict):code="BOOKING_CONFLICT"
	case errors.Is(err,repository.ErrConcurrentUpdate):code="CONCURRENT_UPDATE"
	}
	return Response{Error:&Error{Code:code,Message:publicMessage(code)}}
}
func publicMessage(code string)string{
	switch code{
	case"INVALID_ARGUMENT":return"invalid payment request"
	case"NOT_FOUND":return"payment not found"
	case"IDEMPOTENCY_CONFLICT":return"idempotency key conflicts with an existing payment"
	case"BOOKING_CONFLICT":return"booking already has a payment"
	case"CONCURRENT_UPDATE":return"payment is being updated"
	default:return"internal payment error"
	}
}
