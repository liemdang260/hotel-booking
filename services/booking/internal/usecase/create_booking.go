package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/repository"
)

type CreateBookingInput struct {
	BookingID, UserID, QuoteID, PaymentMethodRef, IdempotencyKey string
}
type CreateBookingOutput struct { BookingID, Status string }

type CreateBookingState struct {
	BookingID, UserID string
	Quote repository.Quote
}
type BookingSagaStore interface {
	CreatePriceAccepted(context.Context, CreateBookingState) error
	MarkInventoryReserved(context.Context,string,repository.Reservation) error
	MarkPaymentRequested(context.Context,string,string) error
	MarkPaymentUnknown(context.Context,string,error) error
	MarkPaymentFailedAndReleased(context.Context,string,string) error
	MarkPaymentSucceeded(context.Context,string,repository.Payment) error
	MarkConfirmationUnknown(context.Context,string,error) error
	MarkCompensationRequired(context.Context,string,string) error
	MarkFailed(context.Context,string,string) error
	MarkConfirmed(context.Context,string,repository.Reservation) error
}
type CreateBookingUsecase struct {
	pricing repository.PricingRepository
	availability repository.AvailabilityRepository
	payment repository.PaymentRepository
	state BookingSagaStore
	now func() time.Time
}
func NewCreateBookingUsecase(p repository.PricingRepository,a repository.AvailabilityRepository,
	pay repository.PaymentRepository,s BookingSagaStore)*CreateBookingUsecase{
	return &CreateBookingUsecase{pricing:p,availability:a,payment:pay,state:s,now:time.Now}
}
func(u *CreateBookingUsecase)Execute(ctx context.Context,in CreateBookingInput)(CreateBookingOutput,error){
	quote,err:=u.pricing.GetQuote(ctx,in.QuoteID)
	if err!=nil{return CreateBookingOutput{},fmt.Errorf("get accepted quote: %w",err)}
	if !u.now().Before(quote.ExpiresAt){return CreateBookingOutput{},repository.ErrQuoteExpired}
	if err=u.state.CreatePriceAccepted(ctx,CreateBookingState{in.BookingID,in.UserID,quote});err!=nil{
		return CreateBookingOutput{},fmt.Errorf("persist accepted quote: %w",err)
	}

	reservation,err:=u.availability.ReserveInventory(ctx,repository.ReserveInventoryCommand{
		BookingID:in.BookingID,HotelID:quote.HotelID,RoomTypeID:quote.RoomTypeID,
		CheckIn:quote.CheckIn,CheckOut:quote.CheckOut,Quantity:quote.RoomQuantity,
	})
	if errors.Is(err,repository.ErrSoldOut)||errors.Is(err,repository.ErrInventoryNotConfigured){
		if saveErr:=u.state.MarkFailed(ctx,in.BookingID,"INVENTORY_UNAVAILABLE");saveErr!=nil{return CreateBookingOutput{},saveErr}
		return CreateBookingOutput{BookingID:in.BookingID,Status:"FAILED"},nil
	}
	if err!=nil{return CreateBookingOutput{},fmt.Errorf("reserve inventory: %w",err)}
	if err=u.state.MarkInventoryReserved(ctx,in.BookingID,reservation);err!=nil{return CreateBookingOutput{},err}

	paymentKey:=in.IdempotencyKey+":payment"
	if err=u.state.MarkPaymentRequested(ctx,in.BookingID,paymentKey);err!=nil{return CreateBookingOutput{},err}
	payment,err:=u.payment.CreatePayment(ctx,repository.CreatePaymentCommand{
		BookingID:in.BookingID,IdempotencyKey:paymentKey,Currency:quote.Currency,
		AmountMinor:quote.TotalMinor,PaymentMethodRef:in.PaymentMethodRef,
	})
	if errors.Is(err,repository.ErrOutcomeUnknown){
		if saveErr:=u.state.MarkPaymentUnknown(ctx,in.BookingID,err);saveErr!=nil{return CreateBookingOutput{},saveErr}
		return CreateBookingOutput{BookingID:in.BookingID,Status:"PAYMENT_UNKNOWN"},nil
	}
	if errors.Is(err,repository.ErrPaymentDeclined){
		return u.compensateConfirmedPaymentFailure(ctx,in.BookingID,reservation.ID,"PAYMENT_DECLINED")
	}
	if err!=nil{return CreateBookingOutput{},fmt.Errorf("request payment: %w",err)}
	if payment.Status==repository.PaymentFailed{
		return u.compensateConfirmedPaymentFailure(ctx,in.BookingID,reservation.ID,"PAYMENT_FAILED")
	}
	if payment.Status==repository.PaymentUnknown||payment.Status==repository.PaymentProcessing{
		if saveErr:=u.state.MarkPaymentUnknown(ctx,in.BookingID,repository.ErrOutcomeUnknown);saveErr!=nil{return CreateBookingOutput{},saveErr}
		return CreateBookingOutput{BookingID:in.BookingID,Status:"PAYMENT_UNKNOWN"},nil
	}
	if payment.Status!=repository.PaymentSucceeded{return CreateBookingOutput{},repository.ErrInvalidRemoteResponse}
	if err=u.state.MarkPaymentSucceeded(ctx,in.BookingID,payment);err!=nil{return CreateBookingOutput{},err}

	confirmed,err:=u.availability.ConfirmReservation(ctx,reservation.ID)
	if errors.Is(err,repository.ErrOutcomeUnknown){
		if saveErr:=u.state.MarkConfirmationUnknown(ctx,in.BookingID,err);saveErr!=nil{return CreateBookingOutput{},saveErr}
		return CreateBookingOutput{BookingID:in.BookingID,Status:"CONFIRMATION_UNKNOWN"},nil
	}
	if errors.Is(err,repository.ErrReservationExpired){
		if saveErr:=u.state.MarkCompensationRequired(ctx,in.BookingID,"REFUND_AFTER_RESERVATION_EXPIRED");saveErr!=nil{return CreateBookingOutput{},saveErr}
		return CreateBookingOutput{BookingID:in.BookingID,Status:"COMPENSATING"},nil
	}
	if err!=nil{return CreateBookingOutput{},fmt.Errorf("confirm reservation: %w",err)}
	if err=u.state.MarkConfirmed(ctx,in.BookingID,confirmed);err!=nil{return CreateBookingOutput{},err}
	return CreateBookingOutput{BookingID:in.BookingID,Status:"CONFIRMED"},nil
}
func(u *CreateBookingUsecase)compensateConfirmedPaymentFailure(ctx context.Context,bookingID,reservationID,reason string)(CreateBookingOutput,error){
	_,err:=u.availability.ReleaseReservation(ctx,reservationID,reason)
	if err!=nil{
		if saveErr:=u.state.MarkCompensationRequired(ctx,bookingID,"RELEASE_HELD_INVENTORY");saveErr!=nil{return CreateBookingOutput{},saveErr}
		return CreateBookingOutput{BookingID:bookingID,Status:"COMPENSATING"},nil
	}
	if err=u.state.MarkPaymentFailedAndReleased(ctx,bookingID,reason);err!=nil{return CreateBookingOutput{},err}
	return CreateBookingOutput{BookingID:bookingID,Status:"PAYMENT_FAILED"},nil
}
