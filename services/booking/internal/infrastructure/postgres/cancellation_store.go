package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
	"github.com/liemdang260/hotel-booking/services/booking/internal/usecase"
)

type CancellationStore struct{ db *sql.DB }
func NewCancellationStore(db *sql.DB)*CancellationStore{return &CancellationStore{db:db}}

const cancellationColumns="id::text,booking_id::text,idempotency_key,request_hash,state,reason,policy_evaluated_at,refund_amount_minor,currency,COALESCE(refund_id,''),COALESCE(failure_code,''),retry_count,next_retry_at,version,created_at,updated_at"
func scanCancellation(s interface{Scan(...any)error})(domain.BookingCancellation,error){
	var c domain.BookingCancellation
	err:=s.Scan(&c.ID,&c.BookingID,&c.IdempotencyKey,&c.RequestHash,&c.State,&c.Reason,&c.PolicyEvaluatedAt,&c.RefundAmountMinor,&c.Currency,&c.RefundID,&c.FailureCode,&c.RetryCount,&c.NextRetryAt,&c.Version,&c.CreatedAt,&c.UpdatedAt)
	return c,err
}
func(s *CancellationStore)BeginOrResume(ctx context.Context,c domain.BookingCancellation)(domain.BookingCancellation,error){
	got,err:=scanCancellation(s.db.QueryRowContext(ctx,"SELECT "+cancellationColumns+" FROM booking_cancellations WHERE idempotency_key=$1",c.IdempotencyKey))
	if err==nil{return got,nil};if !errors.Is(err,sql.ErrNoRows){return domain.BookingCancellation{},err}
	row:=s.db.QueryRowContext(ctx,`INSERT INTO booking_cancellations(id,booking_id,idempotency_key,request_hash,state,reason,policy_evaluated_at,refund_amount_minor,currency,retry_count,version,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,0,$10,$11,$11) RETURNING `+cancellationColumns,c.ID,c.BookingID,c.IdempotencyKey,c.RequestHash,c.State,c.Reason,c.PolicyEvaluatedAt,c.RefundAmountMinor,c.Currency,c.Version,c.CreatedAt)
	got,err=scanCancellation(row);if err!=nil{return domain.BookingCancellation{},fmt.Errorf("begin cancellation: %w",err)};return got,nil
}
func(s *CancellationStore)Load(ctx context.Context,id string)(domain.BookingCancellation,error){
	c,err:=scanCancellation(s.db.QueryRowContext(ctx,"SELECT "+cancellationColumns+" FROM booking_cancellations WHERE id=$1",id));if errors.Is(err,sql.ErrNoRows){return c,domain.ErrNotFound};return c,err
}
func(s *CancellationStore)LoadBooking(ctx context.Context,id string)(usecase.CancellationBooking,error){
	var b usecase.CancellationBooking
	err:=s.db.QueryRowContext(ctx,`SELECT b.id::text,b.status,COALESCE(b.reservation_id,''),COALESCE(b.payment_id,''),p.total_minor,p.currency,
c.booking_id::text,c.policy_code,c.policy_version,c.free_cancel_until,c.refund_basis_points,c.cancellation_fee_minor,c.currency,c.pricing_version,c.created_at
FROM bookings b JOIN booking_price_snapshots p ON p.booking_id=b.id JOIN booking_cancellation_policies c ON c.booking_id=b.id WHERE b.id=$1`,id).Scan(
		&b.ID,&b.Status,&b.ReservationID,&b.PaymentID,&b.TotalMinor,&b.Currency,&b.Policy.BookingID,&b.Policy.PolicyCode,&b.Policy.PolicyVersion,&b.Policy.FreeCancelUntil,&b.Policy.RefundBasisPoints,&b.Policy.CancellationFeeMinor,&b.Policy.Currency,&b.Policy.PricingVersion,&b.Policy.CreatedAt)
	if errors.Is(err,sql.ErrNoRows){return b,domain.ErrNotFound};return b,err
}
func(s *CancellationStore)transition(ctx context.Context,id string,version int64,state domain.CancellationState,extra string)(domain.BookingCancellation,error){
	row:=s.db.QueryRowContext(ctx,`UPDATE booking_cancellations SET state=$3,failure_code=NULLIF($4,''),version=version+1,updated_at=now()
WHERE id=$1 AND version=$2 RETURNING `+cancellationColumns,id,version,state,extra)
	c,err:=scanCancellation(row);if errors.Is(err,sql.ErrNoRows){return c,domain.ErrConcurrentWrite};return c,err
}
func(s *CancellationStore)MarkReservationCancelling(ctx context.Context,id string,v int64)(domain.BookingCancellation,error){
	return s.transition(ctx,id,v,domain.CancellationCancellingReservation,"")
}
func(s *CancellationStore)MarkBookingCancelled(ctx context.Context,id string,v int64)(domain.BookingCancellation,error){
	tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return domain.BookingCancellation{},err};defer func(){ _=tx.Rollback() }()
	c,err:=scanCancellation(tx.QueryRowContext(ctx,`UPDATE booking_cancellations SET state='RESERVATION_CANCELLED',version=version+1,updated_at=now()
WHERE id=$1 AND version=$2 AND state='CANCELLING_RESERVATION' RETURNING `+cancellationColumns,id,v))
	if errors.Is(err,sql.ErrNoRows){return c,domain.ErrConcurrentWrite};if err!=nil{return c,err}
	var b domain.Booking
	err=tx.QueryRowContext(ctx,`UPDATE bookings SET status='CANCELLED',version=version+1,updated_at=now()
WHERE id=$1 AND status='CONFIRMED' RETURNING id::text,user_id,version,updated_at`,c.BookingID).Scan(&b.ID,&b.UserID,&b.Version,&b.UpdatedAt)
	if errors.Is(err,sql.ErrNoRows){return c,domain.ErrConcurrentWrite};if err!=nil{return c,err}
	var eventID string
	if err=tx.QueryRowContext(ctx,"SELECT gen_random_uuid()::text").Scan(&eventID);err!=nil{return c,err}
	event,err:=domain.NewBookingCancelledEvent(eventID,b,c,b.UpdatedAt);if err!=nil{return c,err}
	_,err=tx.ExecContext(ctx,`INSERT INTO booking_outbox_events(id,aggregate_type,aggregate_id,event_type,event_version,payload,status,created_at)
VALUES($1,$2,$3,$4,$5,$6,'PENDING',$7)`,event.ID,event.AggregateType,event.AggregateID,event.EventType,event.EventVersion,event.Payload,event.CreatedAt)
	if err!=nil{return c,err};if err=tx.Commit();err!=nil{return c,err};return c,nil
}
func(s *CancellationStore)MarkRefund(ctx context.Context,id string,v int64,r usecase.CancellationRefund)(domain.BookingCancellation,error){
	state:=domain.CancellationRefundProcessing
	if r.Status==usecase.RefundUnknown{state=domain.CancellationRefundUnknown}
	if r.Status==usecase.RefundSucceeded{state=domain.CancellationCompleted}
	row:=s.db.QueryRowContext(ctx,`UPDATE booking_cancellations SET state=$3,refund_id=NULLIF($4,''),completed_at=CASE WHEN $3='COMPLETED' THEN now() ELSE completed_at END,version=version+1,updated_at=now() WHERE id=$1 AND version=$2 RETURNING `+cancellationColumns,id,v,state,r.ID)
	c,err:=scanCancellation(row);if errors.Is(err,sql.ErrNoRows){return c,domain.ErrConcurrentWrite};return c,err
}
func(s *CancellationStore)CompleteWithoutRefund(ctx context.Context,id string,v int64)(domain.BookingCancellation,error){
	return s.transition(ctx,id,v,domain.CancellationCompleted,"")
}
func(s *CancellationStore)ScheduleRetry(ctx context.Context,id string,v int64,cause error,next time.Time)(domain.BookingCancellation,error){
	code:="TRANSIENT";if cause!=nil{code=cause.Error()}
	row:=s.db.QueryRowContext(ctx,`UPDATE booking_cancellations SET retry_count=retry_count+1,next_retry_at=$3,failure_code=$4,version=version+1,updated_at=now() WHERE id=$1 AND version=$2 RETURNING `+cancellationColumns,id,v,next,code)
	c,err:=scanCancellation(row);if errors.Is(err,sql.ErrNoRows){return c,domain.ErrConcurrentWrite};return c,err
}
func(s *CancellationStore)FindRecoverable(ctx context.Context,now time.Time,limit int)([]domain.BookingCancellation,error){
	if limit<1||limit>100{limit=100}
	rows,err:=s.db.QueryContext(ctx,"SELECT "+cancellationColumns+` FROM booking_cancellations WHERE state IN ('POLICY_APPROVED','CANCELLING_RESERVATION','RESERVATION_CANCELLED','REFUND_PROCESSING','REFUND_UNKNOWN') AND (next_retry_at IS NULL OR next_retry_at<=$1) ORDER BY updated_at LIMIT $2`,now,limit)
	if err!=nil{return nil,err};defer rows.Close();out:=make([]domain.BookingCancellation,0)
	for rows.Next(){c,e:=scanCancellation(rows);if e!=nil{return nil,e};out=append(out,c)};return out,rows.Err()
}
