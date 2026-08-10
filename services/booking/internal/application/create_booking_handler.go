package application

import (
	"context"

	"github.com/liemdang260/hotel-booking/services/booking/internal/usecase"
)
type CreateBookingRequest struct{BookingID,UserID,QuoteID,PaymentMethodRef,IdempotencyKey string}
type CreateBookingResponse struct{BookingID,Status string}
type CreateBookingExecutor interface{
	Execute(context.Context,usecase.CreateBookingInput)(usecase.CreateBookingOutput,error)
}
type CreateBookingHandler struct{usecase CreateBookingExecutor}
func NewCreateBookingHandler(u CreateBookingExecutor)*CreateBookingHandler{return &CreateBookingHandler{usecase:u}}
func(h *CreateBookingHandler)Handle(ctx context.Context,r CreateBookingRequest)(CreateBookingResponse,error){
	out,err:=h.usecase.Execute(ctx,usecase.CreateBookingInput{
		BookingID:r.BookingID,UserID:r.UserID,QuoteID:r.QuoteID,
		PaymentMethodRef:r.PaymentMethodRef,IdempotencyKey:r.IdempotencyKey,
	})
	if err!=nil{return CreateBookingResponse{},err}
	return CreateBookingResponse{BookingID:out.BookingID,Status:out.Status},nil
}
