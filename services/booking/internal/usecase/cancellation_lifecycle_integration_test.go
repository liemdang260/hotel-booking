package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)
func TestCancellationIntegrationZeroRefundSkipsPayment(t *testing.T){
	now:=time.Date(2026,8,11,9,0,0,0,time.UTC);s:=confirmedCancellationFixture(now)
	s.booking.Policy.RefundBasisPoints=0
	u:=NewCancelBookingUsecase(s,cancelAvailFake{s,nil},cancelRefundFake{store:s},cancelIDsFake{});u.now=func()time.Time{return now}
	got,err:=u.Execute(context.Background(),CancelBookingInput{BookingID:s.booking.ID,IdempotencyKey:"zero",RequestHash:"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"})
	if err!=nil{t.Fatal(err)};if got.State!=domain.CancellationCompleted{t.Fatalf("state=%s",got.State)}
	for _,call:=range s.calls{if call=="create-refund"{t.Fatal("zero refund called Payment")}}
}
func TestCancellationIntegrationLostAvailabilityResponseResumesWithoutDoubleRelease(t *testing.T){
	now:=time.Date(2026,8,11,9,0,0,0,time.UTC);s:=confirmedCancellationFixture(now)
	a:=&lostResponseAvailability{store:s}
	u:=NewCancelBookingUsecase(s,a,cancelRefundFake{s,CancellationRefund{ID:"r1",Status:RefundSucceeded},nil},cancelIDsFake{});u.now=func()time.Time{return now}
	_,err:=u.Execute(context.Background(),CancelBookingInput{BookingID:s.booking.ID,IdempotencyKey:"lost",RequestHash:"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"})
	if err==nil{t.Fatal("first lost response must be ambiguous")}
	b:=s.booking;s.booking.Status="CANCELLED"
	got,err:=u.resume(context.Background(),b,s.c);if err!=nil{t.Fatal(err)}
	if got.State!=domain.CancellationCompleted||a.logicalReleases!=1||a.calls!=2{t.Fatalf("state=%s calls=%d releases=%d",got.State,a.calls,a.logicalReleases)}
}
type lostResponseAvailability struct{store *cancelStoreFake;calls,logicalReleases int}
func(a *lostResponseAvailability)CancelBookedReservation(context.Context,string)error{
	a.calls++;if a.logicalReleases==0{a.logicalReleases++}
	if a.calls==1{return errors.New("response lost")}
	return nil
}
func TestCancellationIntegrationRefundUnknownReconcilesSameRefund(t *testing.T){
	now:=time.Date(2026,8,11,9,0,0,0,time.UTC);s:=confirmedCancellationFixture(now)
	r:=&reconcilingRefund{store:s}
	u:=NewCancelBookingUsecase(s,cancelAvailFake{s,nil},r,cancelIDsFake{});u.now=func()time.Time{return now}
	first,err:=u.Execute(context.Background(),CancelBookingInput{BookingID:s.booking.ID,IdempotencyKey:"refund",RequestHash:"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"})
	if err!=nil||first.State!=domain.CancellationRefundUnknown{t.Fatalf("first=%s err=%v",first.State,err)}
	b:=s.booking;b.Status="CANCELLED";second,err:=u.resume(context.Background(),b,first)
	if err!=nil||second.State!=domain.CancellationCompleted||r.creates!=1||r.gets!=1{t.Fatalf("second=%s err=%v creates=%d gets=%d",second.State,err,r.creates,r.gets)}
}
type reconcilingRefund struct{store *cancelStoreFake;creates,gets int}
func(r *reconcilingRefund)CreateRefund(context.Context,string,string,string,int64,string)(CancellationRefund,error){r.creates++;return CancellationRefund{ID:"refund-1",Status:RefundUnknown},nil}
func(r *reconcilingRefund)GetRefund(context.Context,string)(CancellationRefund,error){r.gets++;return CancellationRefund{ID:"refund-1",Status:RefundSucceeded},nil}
