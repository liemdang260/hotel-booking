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

type ids struct{ n int }
func (i *ids) NewID() string { i.n++; if i.n==1{return "pay-1"}; return "attempt-1" }
type clock struct{ now time.Time }
func (c clock) Now() time.Time { return c.now }
type fakeProvider struct{ result provider.ChargeResult; calls int }
func (p *fakeProvider) Charge(context.Context, provider.ChargeRequest) provider.ChargeResult { p.calls++; return p.result }

type memoryPayments struct{ byID map[string]domain.Payment; attempts map[string]domain.Attempt }
func newMemory() *memoryPayments { return &memoryPayments{map[string]domain.Payment{},map[string]domain.Attempt{}} }
func (m *memoryPayments) Create(_ context.Context,p domain.Payment)(domain.Payment,error){
	for _,v:=range m.byID {
		if v.IdempotencyKey==p.IdempotencyKey{return domain.Payment{},repository.ErrIdempotencyConflict}
		if v.BookingID==p.BookingID{return domain.Payment{},repository.ErrBookingConflict}
	}
	m.byID[p.ID]=p;return p,nil
}
func (m *memoryPayments) GetByID(_ context.Context,id string)(domain.Payment,error){p,ok:=m.byID[id];if !ok{return domain.Payment{},repository.ErrPaymentNotFound};return p,nil}
func (m *memoryPayments) GetByIdempotencyKey(_ context.Context,key string)(domain.Payment,error){for _,p:=range m.byID{if p.IdempotencyKey==key{return p,nil}};return domain.Payment{},repository.ErrPaymentNotFound}
func (m *memoryPayments) GetByBookingID(_ context.Context,id string)(domain.Payment,error){for _,p:=range m.byID{if p.BookingID==id{return p,nil}};return domain.Payment{},repository.ErrPaymentNotFound}
func (m *memoryPayments) BeginAttempt(_ context.Context,id string,a domain.Attempt,now time.Time)(domain.Payment,error){
	p,ok:=m.byID[id];if !ok{return domain.Payment{},repository.ErrPaymentNotFound}
	p.Status=domain.StatusProcessing;p.UpdatedAt=now;m.byID[id]=p;m.attempts[a.ID]=a;return p,nil
}
func (m *memoryPayments) CompleteAttempt(_ context.Context,id,attemptID string,out domain.AttemptOutcome,status domain.Status,requestRef,providerRef,failure,raw string,now time.Time)(domain.Payment,error){
	p,ok:=m.byID[id];if !ok{return domain.Payment{},repository.ErrPaymentNotFound}
	a,ok:=m.attempts[attemptID];if !ok{return domain.Payment{},errors.New("attempt not found")}
	a.Outcome=out;a.ProviderRequestRef=requestRef;a.ProviderReference=providerRef;a.FailureCode=failure;a.RawOutcome=raw;a.FinishedAt=&now
	m.attempts[attemptID]=a;p.Status=status;p.ProviderReference=providerRef;p.FailureCode=failure;p.UpdatedAt=now;m.byID[id]=p;return p,nil
}

func input() CreatePaymentInput { return CreatePaymentInput{"booking-1","booking-1-payment",12500,"usd","pm-token"} }
func service(result provider.ChargeResult)(*CreatePayment,*memoryPayments,*fakeProvider){
	r:=newMemory();p:=&fakeProvider{result:result};u:=NewCreatePayment(r,p,&ids{},clock{time.Date(2026,8,10,8,0,0,0,time.UTC)});return u,r,p
}
func TestCreatePaymentSucceedsAndPersistsAttempt(t *testing.T){
	u,r,_:=service(provider.ChargeResult{Outcome:domain.AttemptSucceeded,ProviderReference:"ch-1",RawOutcome:"approved"})
	got,err:=u.Execute(context.Background(),input());if err!=nil{t.Fatal(err)}
	if got.Status!=domain.StatusSucceeded||got.ProviderReference!="ch-1"{t.Fatalf("unexpected payment: %#v",got)}
	if len(r.attempts)!=1{t.Fatalf("attempts=%d",len(r.attempts))}
}
func TestReplayReturnsSamePaymentWithoutSecondCharge(t *testing.T){
	u,_,p:=service(provider.ChargeResult{Outcome:domain.AttemptSucceeded})
	first,err:=u.Execute(context.Background(),input());if err!=nil{t.Fatal(err)}
	second,err:=u.Execute(context.Background(),input());if err!=nil{t.Fatal(err)}
	if first.ID!=second.ID||p.calls!=1{t.Fatalf("ids=%q/%q calls=%d",first.ID,second.ID,p.calls)}
}
func TestChangedIdentityConflicts(t *testing.T){
	u,_,_:=service(provider.ChargeResult{Outcome:domain.AttemptSucceeded})
	if _,err:=u.Execute(context.Background(),input());err!=nil{t.Fatal(err)}
	changed:=input();changed.AmountMinor++
	if _,err:=u.Execute(context.Background(),changed);!errors.Is(err,repository.ErrIdempotencyConflict){t.Fatalf("err=%v",err)}
}
func TestAmbiguousOutcomeIsUnknown(t *testing.T){
	u,_,_:=service(provider.ChargeResult{Outcome:domain.AttemptUnknown,ProviderRequestRef:"req-1",RawOutcome:"timeout"})
	got,err:=u.Execute(context.Background(),input());if err!=nil{t.Fatal(err)}
	if got.Status!=domain.StatusUnknown{t.Fatalf("status=%s",got.Status)}
}
func TestDeclineIsFailed(t *testing.T){
	u,_,_:=service(provider.ChargeResult{Outcome:domain.AttemptDeclined,FailureCode:"card_declined"})
	got,err:=u.Execute(context.Background(),input());if err!=nil{t.Fatal(err)}
	if got.Status!=domain.StatusFailed||got.FailureCode!="card_declined"{t.Fatalf("payment=%#v",got)}
}
