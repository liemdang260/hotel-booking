package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

type cancelStoreFake struct{ booking CancellationBooking; c domain.BookingCancellation; calls []string }
func(s *cancelStoreFake)BeginOrResume(_ context.Context,c domain.BookingCancellation)(domain.BookingCancellation,error){s.calls=append(s.calls,"begin");if s.c.ID!=""{return s.c,nil};s.c=c;return c,nil}
func(s *cancelStoreFake)Load(context.Context,string)(domain.BookingCancellation,error){return s.c,nil}
func(s *cancelStoreFake)LoadBooking(context.Context,string)(CancellationBooking,error){return s.booking,nil}
func(s *cancelStoreFake)MarkReservationCancelling(_ context.Context,_ string,_ int64)(domain.BookingCancellation,error){s.calls=append(s.calls,"mark-cancelling");s.c.State=domain.CancellationCancellingReservation;s.c.Version++;return s.c,nil}
func(s *cancelStoreFake)MarkBookingCancelled(_ context.Context,_ string,_ int64)(domain.BookingCancellation,error){s.calls=append(s.calls,"mark-cancelled");s.booking.Status="CANCELLED";s.c.State=domain.CancellationReservationCancelled;s.c.Version++;return s.c,nil}
func(s *cancelStoreFake)MarkRefund(_ context.Context,_ string,_ int64,r CancellationRefund)(domain.BookingCancellation,error){s.calls=append(s.calls,"refund:"+string(r.Status));s.c.RefundID=r.ID;if r.Status==RefundSucceeded{s.c.State=domain.CancellationCompleted}else if r.Status==RefundUnknown{s.c.State=domain.CancellationRefundUnknown}else{s.c.State=domain.CancellationRefundProcessing};s.c.Version++;return s.c,nil}
func(s *cancelStoreFake)CompleteWithoutRefund(context.Context,string,int64)(domain.BookingCancellation,error){s.c.State=domain.CancellationCompleted;return s.c,nil}
func(s *cancelStoreFake)ScheduleRetry(context.Context,string,int64,error,time.Time)(domain.BookingCancellation,error){s.calls=append(s.calls,"retry");return s.c,nil}
func(s *cancelStoreFake)FindRecoverable(context.Context,time.Time,int)([]domain.BookingCancellation,error){return []domain.BookingCancellation{s.c},nil}
type cancelAvailFake struct{ store *cancelStoreFake; err error }
func(a cancelAvailFake)CancelBookedReservation(context.Context,string)error{a.store.calls=append(a.store.calls,"availability");return a.err}
type cancelRefundFake struct{ store *cancelStoreFake; result CancellationRefund; err error }
func(r cancelRefundFake)CreateRefund(context.Context,string,string,string,int64,string)(CancellationRefund,error){r.store.calls=append(r.store.calls,"create-refund");return r.result,r.err}
func(r cancelRefundFake)GetRefund(context.Context,string)(CancellationRefund,error){r.store.calls=append(r.store.calls,"get-refund");return r.result,r.err}
type cancelIDsFake struct{};func(cancelIDsFake)NewID()string{return "00000000-0000-0000-0000-000000000001"}

func confirmedCancellationFixture(now time.Time)*cancelStoreFake{
	return &cancelStoreFake{booking:CancellationBooking{ID:"00000000-0000-0000-0000-000000000010",Status:"CONFIRMED",ReservationID:"r1",PaymentID:"p1",TotalMinor:10000,Currency:"USD",Policy:domain.CancellationPolicySnapshot{BookingID:"00000000-0000-0000-0000-000000000010",PolicyCode:"NON_REFUNDABLE",PolicyVersion:"v1",FreeCancelUntil:now.Add(time.Hour),RefundBasisPoints:10000,Currency:"USD",PricingVersion:"p1",CreatedAt:now.Add(-time.Hour)}}}
}
func TestCancelBookingOrdersInventoryBeforeBookingAndRefund(t *testing.T){
	now:=time.Date(2026,8,11,7,0,0,0,time.UTC);s:=confirmedCancellationFixture(now)
	u:=NewCancelBookingUsecase(s,cancelAvailFake{s,nil},cancelRefundFake{s,CancellationRefund{ID:"rf1",Status:RefundUnknown},nil},cancelIDsFake{});u.now=func()time.Time{return now}
	got,err:=u.Execute(context.Background(),CancelBookingInput{BookingID:s.booking.ID,IdempotencyKey:"k",RequestHash:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",Reason:"CHANGE_OF_PLAN"})
	if err!=nil{t.Fatal(err)};if got.State!=domain.CancellationRefundUnknown{t.Fatalf("state=%s",got.State)}
	want:=[]string{"begin","mark-cancelling","availability","mark-cancelled","create-refund","refund:UNKNOWN"}
	if len(s.calls)!=len(want){t.Fatalf("calls=%v",s.calls)};for i:=range want{if s.calls[i]!=want[i]{t.Fatalf("calls=%v",s.calls)}}
	if s.booking.Status!="CANCELLED"{t.Fatal("booking must be cancelled before refund settles")}
}
func TestCancelBookingRejectsPreConfirmation(t *testing.T){
	now:=time.Now();s:=confirmedCancellationFixture(now);s.booking.Status="PAYMENT_UNKNOWN"
	u:=NewCancelBookingUsecase(s,cancelAvailFake{s,nil},cancelRefundFake{store:s},cancelIDsFake{})
	_,err:=u.Execute(context.Background(),CancelBookingInput{BookingID:s.booking.ID})
	if !errors.Is(err,domain.ErrBookingNotCancellable){t.Fatalf("err=%v",err)}
	if len(s.calls)!=0{t.Fatalf("side effects=%v",s.calls)}
}
func TestCancelBookingConflictingReplayIsRejectedBeforeRemoteEffects(t *testing.T){
	now:=time.Now();s:=confirmedCancellationFixture(now);s.c=domain.BookingCancellation{ID:"c1",BookingID:s.booking.ID,IdempotencyKey:"k",RequestHash:"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",State:domain.CancellationPolicyApproved}
	u:=NewCancelBookingUsecase(s,cancelAvailFake{s,nil},cancelRefundFake{store:s},cancelIDsFake{});u.now=func()time.Time{return now}
	_,err:=u.Execute(context.Background(),CancelBookingInput{BookingID:s.booking.ID,IdempotencyKey:"k",RequestHash:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if !errors.Is(err,domain.ErrCancellationIdempotencyConflict){t.Fatalf("err=%v",err)}
	if len(s.calls)!=1||s.calls[0]!="begin"{t.Fatalf("remote side effects=%v",s.calls)}
}
