package grpcadapter

import (
	"context"
	"fmt"

	"github.com/liemdang260/hotel-booking/services/booking/internal/repository"
)
type PaymentClient interface {
	CreatePayment(context.Context,*CreatePaymentRequest)(*PaymentResponse,error)
	GetPayment(context.Context,*GetPaymentRequest)(*PaymentResponse,error)
}
type CreatePaymentRequest struct{BookingID,IdempotencyKey,Currency,PaymentMethodRef string;AmountMinor int64}
type GetPaymentRequest struct{PaymentID string}
type PaymentResponse struct{PaymentID,BookingID,Status,ProviderReference string}
type PaymentAdapter struct{client PaymentClient}
func NewPaymentAdapter(c PaymentClient)*PaymentAdapter{return &PaymentAdapter{client:c}}
func(a *PaymentAdapter)CreatePayment(ctx context.Context,c repository.CreatePaymentCommand)(repository.Payment,error){
	r,e:=a.client.CreatePayment(ctx,&CreatePaymentRequest{c.BookingID,c.IdempotencyKey,c.Currency,c.PaymentMethodRef,c.AmountMinor})
	return mapPayment(ctx,r,e,true)
}
func(a *PaymentAdapter)GetPayment(ctx context.Context,id string)(repository.Payment,error){
	r,e:=a.client.GetPayment(ctx,&GetPaymentRequest{id});return mapPayment(ctx,r,e,false)
}
func mapPayment(ctx context.Context,r *PaymentResponse,e error,mutation bool)(repository.Payment,error){
	if e!=nil{return repository.Payment{},mapRPCError(ctx,e,mutation)}
	if r==nil||r.PaymentID==""{return repository.Payment{},fmt.Errorf("%w: malformed payment",repository.ErrInvalidRemoteResponse)}
	s:=repository.PaymentStatus(r.Status)
	switch s{case repository.PaymentPending,repository.PaymentProcessing,repository.PaymentSucceeded,repository.PaymentFailed,repository.PaymentUnknown:
	default:return repository.Payment{},fmt.Errorf("%w: payment status %q",repository.ErrInvalidRemoteResponse,r.Status)}
	return repository.Payment{ID:r.PaymentID,BookingID:r.BookingID,Status:s,ProviderReference:r.ProviderReference},nil
}
