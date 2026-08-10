package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/provider"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

type lookup struct{ result provider.LookupResult; calls int }
func(l *lookup)GetPayment(context.Context,provider.LookupRequest)provider.LookupResult{l.calls++;return l.result}

type reconciliationMemory struct{
	ensures int
	resolved domain.Status
	reschedules int
	exhausted bool
	next time.Time
}
func(r *reconciliationMemory)EnsurePending(context.Context,string,time.Time,int,time.Time)error{r.ensures++;return nil}
func(r *reconciliationMemory)ClaimDue(context.Context,time.Time,time.Time,int)([]repository.ReconciliationJob,error){return nil,nil}
func(r *reconciliationMemory)Resolve(_ context.Context,_ string,_ int64,s domain.Status,_ string,_ string,_ time.Time)(domain.Payment,error){r.resolved=s;return domain.Payment{ID:"pay-1",Status:s},nil}
func(r *reconciliationMemory)Reschedule(_ context.Context,_ string,_ int64,_ int,next time.Time,_ string,_ time.Time)error{r.reschedules++;r.next=next;return nil}
func(r *reconciliationMemory)Exhaust(context.Context,string,int64,int,string,time.Time)error{r.exhausted=true;return nil}

type createStub struct{ payment domain.Payment; calls int }
func(c *createStub)Execute(context.Context,CreatePaymentInput)(domain.Payment,error){c.calls++;return c.payment,nil}

func job(retries,max int)repository.ReconciliationJob{return repository.ReconciliationJob{
	PaymentID:"pay-1",IdempotencyKey:"payment:booking-1",RetryCount:retries,
	MaxAttempts:max,Version:3,
}}
func TestReconciliationResolvesSuccess(t *testing.T){
	r:=&reconciliationMemory{};p:=&lookup{result:provider.LookupResult{Outcome:domain.AttemptSucceeded,ProviderReference:"ch-1"}}
	u:=NewReconcilePayment(r,p,clock{time.Date(2026,8,10,9,0,0,0,time.UTC)},BackoffPolicy{[]time.Duration{time.Second}})
	got,err:=u.Execute(context.Background(),job(0,4));if err!=nil{t.Fatal(err)}
	if !got.Resolved||r.resolved!=domain.StatusSucceeded{t.Fatalf("result=%#v status=%s",got,r.resolved)}
}
func TestReconciliationResolvesDecline(t *testing.T){
	r:=&reconciliationMemory{};p:=&lookup{result:provider.LookupResult{Outcome:domain.AttemptDeclined,FailureCode:"declined"}}
	u:=NewReconcilePayment(r,p,clock{time.Now()},BackoffPolicy{[]time.Duration{time.Second}})
	_,err:=u.Execute(context.Background(),job(0,4));if err!=nil{t.Fatal(err)}
	if r.resolved!=domain.StatusFailed{t.Fatalf("status=%s",r.resolved)}
}
func TestUnknownUsesBoundedBackoff(t *testing.T){
	now:=time.Date(2026,8,10,9,0,0,0,time.UTC);r:=&reconciliationMemory{}
	p:=&lookup{result:provider.LookupResult{Outcome:domain.AttemptUnknown,FailureCode:"timeout"}}
	u:=NewReconcilePayment(r,p,clock{now},BackoffPolicy{[]time.Duration{5*time.Second,15*time.Second,60*time.Second}})
	got,err:=u.Execute(context.Background(),job(0,4));if err!=nil{t.Fatal(err)}
	if r.reschedules!=1||!r.next.Equal(now.Add(5*time.Second))||got.NextRetryAt==nil{t.Fatalf("next=%v result=%#v",r.next,got)}
}
func TestUnknownExhaustsAtLimit(t *testing.T){
	r:=&reconciliationMemory{};p:=&lookup{result:provider.LookupResult{Outcome:domain.AttemptUnknown}}
	u:=NewReconcilePayment(r,p,clock{time.Now()},BackoffPolicy{[]time.Duration{time.Second}})
	got,err:=u.Execute(context.Background(),job(2,3));if err!=nil{t.Fatal(err)}
	if !got.Exhausted||!r.exhausted{t.Fatalf("result=%#v",got)}
}
func TestUnknownCreateReplayEnsuresOneLogicalJob(t *testing.T){
	r:=&reconciliationMemory{};c:=&createStub{payment:domain.Payment{ID:"pay-1",Status:domain.StatusUnknown}}
	u:=NewCreatePaymentWithReconciliation(c,r,clock{time.Now()},5*time.Second,4)
	if _,err:=u.Execute(context.Background(),CreatePaymentInput{});err!=nil{t.Fatal(err)}
	if _,err:=u.Execute(context.Background(),CreatePaymentInput{});err!=nil{t.Fatal(err)}
	if c.calls!=2||r.ensures!=2{t.Fatalf("calls=%d ensures=%d",c.calls,r.ensures)}
	// EnsurePending is repository-idempotent; repeated CreatePayment cannot create a second logical job.
}
func TestBackoffRejectsUnboundedAttempt(t *testing.T){
	_,err:=(BackoffPolicy{[]time.Duration{time.Second}}).Delay(2)
	if !errors.Is(err,ErrInvalidRetryPolicy){t.Fatalf("err=%v",err)}
}
