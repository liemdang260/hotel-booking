//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
	"github.com/liemdang260/hotel-booking/services/booking/internal/usecase"
)

func TestIntegrationCancellationStateAndBookingCancelledOutboxCommitAtomically(t *testing.T){
	db:=openBookingIntegrationDB(t);resetBookingFixture(t,db)
	ctx:=context.Background();now:=time.Date(2026,8,11,9,0,0,0,time.UTC)
	_,err:=db.ExecContext(ctx,`INSERT INTO bookings(id,user_id,hotel_id,room_type_id,check_in,check_out,guest_count,room_quantity,status,reservation_id,payment_id,version,created_at,updated_at)
VALUES('00000000-0000-0000-0000-000000009001','u1','h1','rt1','2026-09-01','2026-09-02',1,1,'CONFIRMED','r1','p1',7,$1,$1)`,now)
	if err!=nil{t.Fatal(err)}
	tx,err:=db.BeginTx(ctx,nil);if err!=nil{t.Fatal(err)};defer tx.Rollback()
	_,err=tx.ExecContext(ctx,`INSERT INTO booking_cancellations(id,booking_id,idempotency_key,request_hash,state,reason,policy_evaluated_at,refund_amount_minor,currency,version,created_at,updated_at)
VALUES('00000000-0000-0000-0000-000000009002','00000000-0000-0000-0000-000000009001','cancel-1',repeat('a',64),'RESERVATION_CANCELLED','CHANGE_OF_PLAN',$1,10000,'USD',2,$1,$1)`,now)
	if err!=nil{t.Fatal(err)}
	res,err:=tx.ExecContext(ctx,`UPDATE bookings SET status='CANCELLED',version=version+1,updated_at=$1 WHERE id='00000000-0000-0000-0000-000000009001' AND status='CONFIRMED' AND version=7`,now)
	if err!=nil{t.Fatal(err)};if n,_:=res.RowsAffected();n!=1{t.Fatalf("booking updates=%d",n)}
	b:=domain.Booking{ID:"00000000-0000-0000-0000-000000009001",UserID:"u1",Version:7}
	c:=domain.BookingCancellation{ID:"00000000-0000-0000-0000-000000009002",IdempotencyKey:"cancel-1",RefundAmountMinor:10000,Currency:"USD"}
	event,err:=domain.NewBookingCancelledEvent("00000000-0000-0000-0000-000000009003",b,c,now);if err!=nil{t.Fatal(err)}
	if !json.Valid(event.Payload){t.Fatal("invalid envelope")}
	_,err=tx.ExecContext(ctx,`INSERT INTO booking_outbox_events(id,aggregate_type,aggregate_id,event_type,event_version,payload,status,created_at)
VALUES($1,$2,$3,$4,$5,$6,'PENDING',$7)`,event.ID,event.AggregateType,event.AggregateID,event.EventType,event.EventVersion,event.Payload,event.CreatedAt)
	if err!=nil{t.Fatal(err)};if err=tx.Commit();err!=nil{t.Fatal(err)}
	var status string;var events int
	if err=db.QueryRowContext(ctx,`SELECT status FROM bookings WHERE id='00000000-0000-0000-0000-000000009001'`).Scan(&status);err!=nil{t.Fatal(err)}
	if err=db.QueryRowContext(ctx,`SELECT count(*) FROM booking_outbox_events WHERE aggregate_id='00000000-0000-0000-0000-000000009001' AND event_type='BookingCancelled'`).Scan(&events);err!=nil{t.Fatal(err)}
	if status!="CANCELLED"||events!=1{t.Fatalf("status=%s events=%d",status,events)}
	store:=NewCancellationStore(db)
	completed,err:=store.MarkRefund(ctx,"00000000-0000-0000-0000-000000009002",2,usecase.CancellationRefund{ID:"refund-1",Status:usecase.RefundSucceeded})
	if err!=nil{t.Fatal(err)};if completed.State!=domain.CancellationCompleted{t.Fatalf("refund state=%s",completed.State)}
	var refundEvents int
	if err=db.QueryRowContext(ctx,`SELECT count(*) FROM booking_outbox_events WHERE aggregate_id='00000000-0000-0000-0000-000000009001' AND event_type='BookingRefundCompleted'`).Scan(&refundEvents);err!=nil{t.Fatal(err)}
	if refundEvents!=1{t.Fatalf("refund completion events=%d",refundEvents)}
	_,err=db.ExecContext(ctx,`INSERT INTO booking_cancellations(booking_id,idempotency_key,request_hash,state,reason,policy_evaluated_at,refund_amount_minor,currency)
VALUES('00000000-0000-0000-0000-000000009001','cancel-2',repeat('b',64),'POLICY_APPROVED','OTHER',$1,10000,'USD')`,now)
	if err==nil{t.Fatal("second active cancellation must violate unique invariant")}
}
