package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

type PaymentRepository struct { db *sql.DB }
func NewPaymentRepository(db *sql.DB) *PaymentRepository { return &PaymentRepository{db:db} }

const paymentColumns = "id, booking_id, idempotency_key, amount_minor, currency, payment_method_ref, status, provider_reference, failure_code, created_at, updated_at"

func scanPayment(s interface{ Scan(...any) error }) (domain.Payment,error) {
	var p domain.Payment
	err:=s.Scan(&p.ID,&p.BookingID,&p.IdempotencyKey,&p.AmountMinor,&p.Currency,&p.PaymentMethodRef,&p.Status,&p.ProviderReference,&p.FailureCode,&p.CreatedAt,&p.UpdatedAt)
	return p,err
}
func (r *PaymentRepository) Create(ctx context.Context,p domain.Payment)(domain.Payment,error){
	row:=r.db.QueryRowContext(ctx,`INSERT INTO payments (`+paymentColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+paymentColumns,
		p.ID,p.BookingID,p.IdempotencyKey,p.AmountMinor,p.Currency,p.PaymentMethodRef,p.Status,p.ProviderReference,p.FailureCode,p.CreatedAt,p.UpdatedAt)
	got,err:=scanPayment(row);if err!=nil{return domain.Payment{},mapWriteError(err)};return got,nil
}
func (r *PaymentRepository) GetByID(ctx context.Context,id string)(domain.Payment,error){return r.get(ctx,"id",id)}
func (r *PaymentRepository) GetByIdempotencyKey(ctx context.Context,key string)(domain.Payment,error){return r.get(ctx,"idempotency_key",key)}
func (r *PaymentRepository) GetByBookingID(ctx context.Context,id string)(domain.Payment,error){return r.get(ctx,"booking_id",id)}
func (r *PaymentRepository) get(ctx context.Context,column,value string)(domain.Payment,error){
	switch column{case"id","idempotency_key","booking_id":default:return domain.Payment{},fmt.Errorf("invalid lookup")}
	p,err:=scanPayment(r.db.QueryRowContext(ctx,"SELECT "+paymentColumns+" FROM payments WHERE "+column+" = $1",value))
	if errors.Is(err,sql.ErrNoRows){return domain.Payment{},repository.ErrPaymentNotFound};return p,err
}
func (r *PaymentRepository) BeginAttempt(ctx context.Context,paymentID string,a domain.Attempt,now time.Time)(domain.Payment,error){
	tx,err:=r.db.BeginTx(ctx,nil);if err!=nil{return domain.Payment{},err};defer tx.Rollback()
	res,err:=tx.ExecContext(ctx,`UPDATE payments SET status='PROCESSING',updated_at=$2 WHERE id=$1 AND status IN ('PENDING','UNKNOWN')`,paymentID,now)
	if err!=nil{return domain.Payment{},err};n,_:=res.RowsAffected();if n!=1{return domain.Payment{},repository.ErrConcurrentUpdate}
	_,err=tx.ExecContext(ctx,`INSERT INTO payment_attempts (id,payment_id,idempotency_key,provider_request_ref,provider_reference,outcome,failure_code,raw_outcome,started_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,a.ID,a.PaymentID,a.IdempotencyKey,a.ProviderRequestRef,a.ProviderReference,a.Outcome,a.FailureCode,a.RawOutcome,a.StartedAt)
	if err!=nil{return domain.Payment{},err}
	p,err:=scanPayment(tx.QueryRowContext(ctx,"SELECT "+paymentColumns+" FROM payments WHERE id=$1",paymentID));if err!=nil{return domain.Payment{},err}
	if err=tx.Commit();err!=nil{return domain.Payment{},err};return p,nil
}
func (r *PaymentRepository) CompleteAttempt(ctx context.Context,paymentID,attemptID string,out domain.AttemptOutcome,status domain.Status,requestRef,providerRef,failure,raw string,now time.Time)(domain.Payment,error){
	tx,err:=r.db.BeginTx(ctx,nil);if err!=nil{return domain.Payment{},err};defer tx.Rollback()
	res,err:=tx.ExecContext(ctx,`UPDATE payment_attempts SET outcome=$3,provider_request_ref=$4,provider_reference=$5,failure_code=$6,raw_outcome=$7,finished_at=$8 WHERE id=$1 AND payment_id=$2 AND finished_at IS NULL`,attemptID,paymentID,out,requestRef,providerRef,failure,raw,now)
	if err!=nil{return domain.Payment{},err};n,_:=res.RowsAffected();if n!=1{return domain.Payment{},repository.ErrConcurrentUpdate}
	res,err=tx.ExecContext(ctx,`UPDATE payments SET status=$2,provider_reference=$3,failure_code=$4,updated_at=$5 WHERE id=$1 AND status='PROCESSING'`,paymentID,status,providerRef,failure,now)
	if err!=nil{return domain.Payment{},err};n,_=res.RowsAffected();if n!=1{return domain.Payment{},repository.ErrConcurrentUpdate}
	p,err:=scanPayment(tx.QueryRowContext(ctx,"SELECT "+paymentColumns+" FROM payments WHERE id=$1",paymentID));if err!=nil{return domain.Payment{},err}
	if err=tx.Commit();err!=nil{return domain.Payment{},err};return p,nil
}

type sqlStateError interface { SQLState() string }
func mapWriteError(err error) error {
	var state sqlStateError
	if errors.As(err,&state)&&state.SQLState()=="23505" {
		switch {
		case strings.Contains(err.Error(),"payments_idempotency_key_key"):
			return repository.ErrIdempotencyConflict
		case strings.Contains(err.Error(),"payments_booking_id_key"):
			return repository.ErrBookingConflict
		}
	}
	return fmt.Errorf("create payment: %w",err)
}
