package application

import (
	"context"
	"testing"

	"github.com/liemdang260/hotel-booking/services/booking/internal/usecase"
)
type executorStub struct{calls int;input usecase.CreateBookingInput}
func(s *executorStub)Execute(_ context.Context,in usecase.CreateBookingInput)(usecase.CreateBookingOutput,error){s.calls++;s.input=in;return usecase.CreateBookingOutput{BookingID:in.BookingID,Status:"CONFIRMED"},nil}
func TestHandlerOnlyMapsAndDelegates(t *testing.T){
	s:=&executorStub{};h:=NewCreateBookingHandler(s)
	out,err:=h.Handle(context.Background(),CreateBookingRequest{BookingID:"b1",UserID:"u1",QuoteID:"q1",PaymentMethodRef:"pm1",IdempotencyKey:"k1"})
	if err!=nil{t.Fatal(err)}
	if s.calls!=1||s.input.QuoteID!="q1"||out.Status!="CONFIRMED"{t.Fatalf("calls=%d input=%+v out=%+v",s.calls,s.input,out)}
}
