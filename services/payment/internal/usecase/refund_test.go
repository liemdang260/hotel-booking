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

type refundRepoFake struct {
	refund domain.Refund
	attempts int
}
func (r *refundRepoFake) Create(_ context.Context, v domain.Refund) (domain.Refund,error) {
	if r.refund.ID!="" { return domain.Refund{},repository.ErrRefundIdempotencyConflict }
	r.refund=v; return v,nil
}
func (r *refundRepoFake) GetByID(_ context.Context,id string)(domain.Refund,error){if r.refund.ID!=id{return domain.Refund{},repository.ErrRefundNotFound};return r.refund,nil}
func (r *refundRepoFake) GetByIdempotencyKey(_ context.Context,key string)(domain.Refund,error){if r.refund.IdempotencyKey!=key{return domain.Refund{},repository.ErrRefundNotFound};return r.refund,nil}
func (r *refundRepoFake) BeginAttempt(_ context.Context,_ string,_ domain.RefundAttempt,now time.Time)(domain.Refund,error){r.attempts++;r.refund.Status=domain.RefundProcessing;r.refund.UpdatedAt=now;return r.refund,nil}
func (r *refundRepoFake) CompleteAttempt(_ context.Context,_,_ string,_ domain.AttemptOutcome,s domain.RefundStatus,_,ref,failure,_ string,now time.Time)(domain.Refund,error){r.refund.Status=s;r.refund.ProviderReference=ref;r.refund.FailureCode=failure;r.refund.UpdatedAt=now;return r.refund,nil}
func (r *refundRepoFake) ResolveUnknown(_ context.Context,_ string,s domain.RefundStatus,ref,failure string,now time.Time)(domain.Refund,error){r.refund.Status=s;r.refund.ProviderReference=ref;r.refund.FailureCode=failure;r.refund.UpdatedAt=now;return r.refund,nil}

type refundProviderFake struct { create,lookup provider.RefundResult; calls int }
func (p *refundProviderFake) Refund(context.Context,provider.RefundRequest)provider.RefundResult{p.calls++;return p.create}
func (p *refundProviderFake) GetRefund(context.Context,provider.RefundRequest)provider.RefundResult{return p.lookup}
type refundIDs struct{ n int }
func(i *refundIDs)NewID()string{i.n++;if i.n==1{return"refund-1"};return"attempt-1"}
type refundClock struct{}
func(refundClock)Now()time.Time{return time.Date(2026,8,11,7,0,0,0,time.UTC)}

func TestCreateRefundTimeoutReplayAndRecovery(t *testing.T){
	repo:=&refundRepoFake{}
	p:=&refundProviderFake{create:provider.RefundResult{Outcome:domain.AttemptUnknown,FailureCode:"TIMEOUT"},lookup:provider.RefundResult{Outcome:domain.AttemptSucceeded,ProviderReference:"provider-refund-1"}}
	create:=NewCreateRefund(repo,p,&refundIDs{},refundClock{})
	in:=CreateRefundInput{PaymentID:"payment-1",BookingID:"booking-1",IdempotencyKey:"refund:booking-1:cancel-1",AmountMinor:25000,Currency:"usd"}
	first,err:=create.Execute(context.Background(),in);if err!=nil{t.Fatal(err)}
	if first.Status!=domain.RefundUnknown{t.Fatalf("status=%s",first.Status)}
	replay,err:=create.Execute(context.Background(),in);if err!=nil{t.Fatal(err)}
	if replay.ID!=first.ID||p.calls!=1||repo.attempts!=1{t.Fatalf("replay duplicated refund calls=%d attempts=%d",p.calls,repo.attempts)}
	got,err:=NewGetRefund(repo,p,refundClock{}).Execute(context.Background(),first.ID);if err!=nil{t.Fatal(err)}
	if got.Status!=domain.RefundSucceeded||got.ProviderReference!="provider-refund-1"{t.Fatalf("reconciled=%+v",got)}
}
func TestCreateRefundConflictingReplay(t *testing.T){
	repo:=&refundRepoFake{}
	p:=&refundProviderFake{create:provider.RefundResult{Outcome:domain.AttemptSucceeded}}
	create:=NewCreateRefund(repo,p,&refundIDs{},refundClock{})
	in:=CreateRefundInput{PaymentID:"payment-1",BookingID:"booking-1",IdempotencyKey:"key",AmountMinor:100,Currency:"USD"}
	if _,err:=create.Execute(context.Background(),in);err!=nil{t.Fatal(err)}
	in.AmountMinor=200
	if _,err:=create.Execute(context.Background(),in);!errors.Is(err,repository.ErrRefundIdempotencyConflict){t.Fatalf("err=%v",err)}
}
