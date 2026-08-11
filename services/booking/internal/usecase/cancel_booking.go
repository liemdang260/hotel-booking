package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

type CancellationBooking struct {
	ID, Status, ReservationID, PaymentID string
	TotalMinor int64
	Currency string
	Policy domain.CancellationPolicySnapshot
}

type CancellationRefundStatus string
const (
	RefundProcessing CancellationRefundStatus="PROCESSING"
	RefundSucceeded CancellationRefundStatus="SUCCEEDED"
	RefundFailed CancellationRefundStatus="FAILED"
	RefundUnknown CancellationRefundStatus="UNKNOWN"
)

type CancellationRefund struct{ ID string; Status CancellationRefundStatus }

type CancellationStore interface {
	BeginOrResume(context.Context, domain.BookingCancellation) (domain.BookingCancellation,error)
	Load(context.Context,string)(domain.BookingCancellation,error)
	LoadBooking(context.Context,string)(CancellationBooking,error)
	MarkReservationCancelling(context.Context,string,int64)(domain.BookingCancellation,error)
	MarkBookingCancelled(context.Context,string,int64)(domain.BookingCancellation,error)
	MarkRefund(context.Context,string,int64,CancellationRefund)(domain.BookingCancellation,error)
	CompleteWithoutRefund(context.Context,string,int64)(domain.BookingCancellation,error)
	ScheduleRetry(context.Context,string,int64,error,time.Time)(domain.BookingCancellation,error)
	FindRecoverable(context.Context,time.Time,int)([]domain.BookingCancellation,error)
}

type BookedReservationCanceller interface{ CancelBookedReservation(context.Context,string)(error) }
type CancellationRefunds interface {
	CreateRefund(context.Context,string,string,string,int64,string)(CancellationRefund,error)
	GetRefund(context.Context,string)(CancellationRefund,error)
}
type CancellationIDs interface{ NewID()string }

type CancelBookingInput struct{ BookingID,IdempotencyKey,RequestHash,Reason string }
type CancelBookingUsecase struct {
	store CancellationStore; availability BookedReservationCanceller; refunds CancellationRefunds
	ids CancellationIDs; now func()time.Time; retryDelay time.Duration
}
func NewCancelBookingUsecase(s CancellationStore,a BookedReservationCanceller,r CancellationRefunds,ids CancellationIDs)*CancelBookingUsecase{
	return &CancelBookingUsecase{store:s,availability:a,refunds:r,ids:ids,now:time.Now,retryDelay:time.Minute}
}

func (u *CancelBookingUsecase) Execute(ctx context.Context,in CancelBookingInput)(domain.BookingCancellation,error){
	b,err:=u.store.LoadBooking(ctx,in.BookingID);if err!=nil{return domain.BookingCancellation{},err}
	if b.Status!="CONFIRMED"{return domain.BookingCancellation{},domain.ErrBookingNotCancellable}
	evaluatedAt:=u.now().UTC()
	refund,err:=b.Policy.CalculateRefund(b.TotalMinor,evaluatedAt);if err!=nil{return domain.BookingCancellation{},err}
	c,err:=domain.NewBookingCancellation(u.ids.NewID(),b.ID,in.IdempotencyKey,in.RequestHash,in.Reason,b.Currency,evaluatedAt,refund)
	if err!=nil{return domain.BookingCancellation{},err}
	c,err=u.store.BeginOrResume(ctx,c)
	if err!=nil{return domain.BookingCancellation{},err}
	if !c.SameRequest(in.RequestHash){return domain.BookingCancellation{},domain.ErrCancellationIdempotencyConflict}
	return u.resume(ctx,b,c)
}

func(u *CancelBookingUsecase) resume(ctx context.Context,b CancellationBooking,c domain.BookingCancellation)(domain.BookingCancellation,error){
	switch c.State {
	case domain.CancellationCompleted:
		return c,nil
	case domain.CancellationPolicyApproved,domain.CancellationCancellingReservation:
		if c.State==domain.CancellationPolicyApproved {
			var err error;c,err=u.store.MarkReservationCancelling(ctx,c.ID,c.Version);if err!=nil{return c,err}
		}
		if err:=u.availability.CancelBookedReservation(ctx,b.ReservationID);err!=nil {
			_,_ = u.store.ScheduleRetry(ctx,c.ID,c.Version,err,u.now().Add(u.retryDelay))
			return c,fmt.Errorf("cancel booked reservation: %w",err)
		}
		var err error;c,err=u.store.MarkBookingCancelled(ctx,c.ID,c.Version);if err!=nil{return c,err}
		fallthrough
	case domain.CancellationReservationCancelled,domain.CancellationRefundProcessing,domain.CancellationRefundUnknown:
		if c.RefundAmountMinor==0 { return u.store.CompleteWithoutRefund(ctx,c.ID,c.Version) }
		var r CancellationRefund
		var err error
		if c.RefundID=="" {
			r,err=u.refunds.CreateRefund(ctx,b.PaymentID,b.ID,"cancellation:"+c.ID,c.RefundAmountMinor,c.Currency)
		} else {
			r,err=u.refunds.GetRefund(ctx,c.RefundID)
		}
		if err!=nil {
			if errors.Is(err,context.DeadlineExceeded){r=CancellationRefund{ID:c.RefundID,Status:RefundUnknown}} else {
				_,_=u.store.ScheduleRetry(ctx,c.ID,c.Version,err,u.now().Add(u.retryDelay));return c,err
			}
		}
		return u.store.MarkRefund(ctx,c.ID,c.Version,r)
	default:
		return c,nil
	}
}

type RecoverCancellationsUsecase struct{ cancel *CancelBookingUsecase; limit int }
func NewRecoverCancellationsUsecase(c *CancelBookingUsecase)*RecoverCancellationsUsecase{return &RecoverCancellationsUsecase{cancel:c,limit:100}}
func(u *RecoverCancellationsUsecase) Execute(ctx context.Context)error{
	items,err:=u.cancel.store.FindRecoverable(ctx,u.cancel.now(),u.limit);if err!=nil{return err}
	for _,c:=range items {
		b,e:=u.cancel.store.LoadBooking(ctx,c.BookingID);if e!=nil{return e}
		if _,e=u.cancel.resume(ctx,b,c);e!=nil && !errors.Is(e,context.Canceled)&&!errors.Is(e,context.DeadlineExceeded){continue}
		if e!=nil{return e}
	}
	return nil
}
