package postgres

import (
	"context"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

func(r *PaymentRepository)EnsurePending(ctx context.Context,paymentID string,next time.Time,max int,now time.Time)error{
	_,err:=r.db.ExecContext(ctx,`INSERT INTO payment_reconciliations
(payment_id,status,retry_count,max_attempts,next_retry_at,version,created_at,updated_at)
VALUES($1,'PENDING',0,$2,$3,1,$4,$4)
ON CONFLICT(payment_id) DO NOTHING`,paymentID,max,next,now)
	return err
}
func(r *PaymentRepository)ClaimDue(ctx context.Context,now,leaseUntil time.Time,limit int)([]repository.ReconciliationJob,error){
	if limit<=0{return nil,nil}
	tx,err:=r.db.BeginTx(ctx,nil);if err!=nil{return nil,err};defer tx.Rollback()
	rows,err:=tx.QueryContext(ctx,`WITH due AS (
 SELECT payment_id FROM payment_reconciliations
 WHERE (status='PENDING' AND next_retry_at<=$1)
    OR (status='CLAIMED' AND lease_until<=$1)
 ORDER BY next_retry_at,payment_id
 LIMIT $2 FOR UPDATE SKIP LOCKED
)
UPDATE payment_reconciliations r SET status='CLAIMED',lease_until=$3,version=r.version+1,updated_at=$1
FROM due WHERE r.payment_id=due.payment_id
RETURNING r.payment_id,
 (SELECT p.idempotency_key FROM payments p WHERE p.id=r.payment_id),
 (SELECT p.provider_reference FROM payments p WHERE p.id=r.payment_id),
 r.status,r.retry_count,r.max_attempts,r.next_retry_at,r.lease_until,
 r.last_error_code,r.version,r.created_at,r.updated_at`,now,limit,leaseUntil)
	if err!=nil{return nil,err}
	var jobs []repository.ReconciliationJob
	for rows.Next(){
		var j repository.ReconciliationJob
		if err:=rows.Scan(&j.PaymentID,&j.IdempotencyKey,&j.ProviderReference,&j.Status,
			&j.RetryCount,&j.MaxAttempts,&j.NextRetryAt,&j.LeaseUntil,
			&j.LastErrorCode,&j.Version,&j.CreatedAt,&j.UpdatedAt);err!=nil{
			rows.Close();return nil,err
		}
		jobs=append(jobs,j)
	}
	if err:=rows.Err();err!=nil{rows.Close();return nil,err}
	if err:=rows.Close();err!=nil{return nil,err}
	if err:=tx.Commit();err!=nil{return nil,err}
	return jobs,nil
}
func(r *PaymentRepository)Resolve(ctx context.Context,paymentID string,version int64,status domain.Status,providerRef,failure string,now time.Time)(domain.Payment,error){
	if status!=domain.StatusSucceeded&&status!=domain.StatusFailed{return domain.Payment{},domain.ErrInvalidTransition}
	tx,err:=r.db.BeginTx(ctx,nil);if err!=nil{return domain.Payment{},err};defer tx.Rollback()
	res,err:=tx.ExecContext(ctx,`UPDATE payments SET status=$2,provider_reference=$3,failure_code=$4,updated_at=$5 WHERE id=$1 AND status='UNKNOWN'`,paymentID,status,providerRef,failure,now)
	if err!=nil{return domain.Payment{},err};n,_:=res.RowsAffected();if n!=1{return domain.Payment{},repository.ErrConcurrentUpdate}
	res,err=tx.ExecContext(ctx,`UPDATE payment_reconciliations SET status='RESOLVED',lease_until=NULL,version=version+1,updated_at=$3 WHERE payment_id=$1 AND version=$2 AND status='CLAIMED'`,paymentID,version,now)
	if err!=nil{return domain.Payment{},err};n,_=res.RowsAffected();if n!=1{return domain.Payment{},repository.ErrConcurrentUpdate}
	p,err:=scanPayment(tx.QueryRowContext(ctx,"SELECT "+paymentColumns+" FROM payments WHERE id=$1",paymentID));if err!=nil{return domain.Payment{},err}
	if err=tx.Commit();err!=nil{return domain.Payment{},err};return p,nil
}
func(r *PaymentRepository)Reschedule(ctx context.Context,paymentID string,version int64,retries int,next time.Time,lastError string,now time.Time)error{
	res,err:=r.db.ExecContext(ctx,`UPDATE payment_reconciliations SET status='PENDING',retry_count=$3,next_retry_at=$4,lease_until=NULL,last_error_code=$5,version=version+1,updated_at=$6 WHERE payment_id=$1 AND version=$2 AND status='CLAIMED'`,paymentID,version,retries,next,lastError,now)
	if err!=nil{return err};n,_:=res.RowsAffected();if n!=1{return repository.ErrConcurrentUpdate};return nil
}
func(r *PaymentRepository)Exhaust(ctx context.Context,paymentID string,version int64,retries int,lastError string,now time.Time)error{
	res,err:=r.db.ExecContext(ctx,`UPDATE payment_reconciliations SET status='EXHAUSTED',retry_count=$3,lease_until=NULL,last_error_code=$4,version=version+1,updated_at=$5 WHERE payment_id=$1 AND version=$2 AND status='CLAIMED'`,paymentID,version,retries,lastError,now)
	if err!=nil{return err};n,_:=res.RowsAffected();if n!=1{return repository.ErrConcurrentUpdate};return nil
}

var _ repository.ReconciliationRepository=(*PaymentRepository)(nil)
