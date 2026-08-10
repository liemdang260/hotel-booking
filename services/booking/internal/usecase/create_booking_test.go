package usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/repository"
)
type pricingStub struct{quote repository.Quote;log *[]string}
func(s pricingStub)GetQuote(context.Context,string)(repository.Quote,error){*s.log=append(*s.log,"remote:quote");return s.quote,nil}
type availabilityStub struct{reserveErr,confirmErr,releaseErr error;log *[]string}
func(s availabilityStub)ReserveInventory(context.Context,repository.ReserveInventoryCommand)(repository.Reservation,error){*s.log=append(*s.log,"remote:reserve");return repository.Reservation{ID:"r1",Status:repository.ReservationHeld},s.reserveErr}
func(s availabilityStub)GetReservation(context.Context,string)(repository.Reservation,error){return repository.Reservation{},nil}
func(s availabilityStub)ConfirmReservation(context.Context,string)(repository.Reservation,error){*s.log=append(*s.log,"remote:confirm");return repository.Reservation{ID:"r1",Status:repository.ReservationBooked},s.confirmErr}
func(s availabilityStub)ReleaseReservation(context.Context,string,string)(repository.Reservation,error){*s.log=append(*s.log,"remote:release");return repository.Reservation{ID:"r1",Status:repository.ReservationReleased},s.releaseErr}
type paymentStub struct{payment repository.Payment;err error;log *[]string}
func(s paymentStub)CreatePayment(context.Context,repository.CreatePaymentCommand)(repository.Payment,error){*s.log=append(*s.log,"remote:payment");return s.payment,s.err}
func(s paymentStub)GetPayment(context.Context,string)(repository.Payment,error){return repository.Payment{},nil}
type stateStub struct{log *[]string}
func(s stateStub)add(v string)error{*s.log=append(*s.log,"persist:"+v);return nil}
func(s stateStub)CreatePriceAccepted(context.Context,CreateBookingState)error{return s.add("price")}
func(s stateStub)MarkInventoryReserved(context.Context,string,repository.Reservation)error{return s.add("inventory")}
func(s stateStub)MarkPaymentRequested(context.Context,string,string)error{return s.add("payment-request")}
func(s stateStub)MarkPaymentUnknown(context.Context,string,error)error{return s.add("payment-unknown")}
func(s stateStub)MarkPaymentFailedAndReleased(context.Context,string,string)error{return s.add("payment-failed-released")}
func(s stateStub)MarkPaymentSucceeded(context.Context,string,repository.Payment)error{return s.add("payment-succeeded")}
func(s stateStub)MarkConfirmationUnknown(context.Context,string,error)error{return s.add("confirmation-unknown")}
func(s stateStub)MarkCompensationRequired(context.Context,string,string)error{return s.add("compensating")}
func(s stateStub)MarkFailed(context.Context,string,string)error{return s.add("failed")}
func(s stateStub)MarkConfirmed(context.Context,string,repository.Reservation)error{return s.add("confirmed")}
func fixture(log *[]string,a availabilityStub,p paymentStub)*CreateBookingUsecase{
	q:=repository.Quote{ID:"q1",HotelID:"h",RoomTypeID:"rt",Currency:"USD",RoomQuantity:1,TotalMinor:100,ExpiresAt:time.Now().Add(time.Hour)}
	u:=NewCreateBookingUsecase(pricingStub{q,log},a,p,stateStub{log});u.now=func()time.Time{return time.Now()};return u
}
func TestHappyPathPersistsBetweenRemoteCalls(t *testing.T){
	var log []string
	u:=fixture(&log,availabilityStub{log:&log},paymentStub{payment:repository.Payment{ID:"p1",Status:repository.PaymentSucceeded},log:&log})
	out,err:=u.Execute(context.Background(),CreateBookingInput{BookingID:"b1",QuoteID:"q1",IdempotencyKey:"k"})
	if err!=nil||out.Status!="CONFIRMED"{t.Fatalf("out=%+v err=%v",out,err)}
	want:=[]string{"remote:quote","persist:price","remote:reserve","persist:inventory","persist:payment-request","remote:payment","persist:payment-succeeded","remote:confirm","persist:confirmed"}
	if !reflect.DeepEqual(log,want){t.Fatalf("durable order=%v",log)}
}
func TestPaymentTimeoutPersistsUnknownWithoutRelease(t *testing.T){
	var log []string
	u:=fixture(&log,availabilityStub{log:&log},paymentStub{err:repository.ErrOutcomeUnknown,log:&log})
	out,err:=u.Execute(context.Background(),CreateBookingInput{BookingID:"b1",QuoteID:"q1",IdempotencyKey:"k"})
	if err!=nil||out.Status!="PAYMENT_UNKNOWN"{t.Fatalf("out=%+v err=%v",out,err)}
	for _,v:=range log{if v=="remote:release"{t.Fatal("ambiguous payment was compensated")}}
	if log[len(log)-1]!="persist:payment-unknown"{t.Fatalf("log=%v",log)}
}
func TestSoldOutIsTerminalWithoutPayment(t *testing.T){
	var log []string
	u:=fixture(&log,availabilityStub{reserveErr:repository.ErrSoldOut,log:&log},paymentStub{log:&log})
	out,err:=u.Execute(context.Background(),CreateBookingInput{BookingID:"b1",QuoteID:"q1"})
	if err!=nil||out.Status!="FAILED"{t.Fatalf("out=%+v err=%v",out,err)}
	for _,v:=range log{if v=="remote:payment"{t.Fatal("payment called after sold out")}}
	if !errors.Is(repository.ErrSoldOut,repository.ErrSoldOut){t.Fatal("sentinel mismatch")}
}
