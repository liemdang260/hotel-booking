//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

func TestIntegrationRefundPersistenceIdempotencyAndUnknownRecovery(t *testing.T) {
	db:=openPaymentIntegrationDB(t)
	resetPaymentFixture(t,db)
	payments:=NewPaymentRepository(db)
	p:=newIntegrationPayment(t,"payment-r1","booking-r1","payment-key-r1")
	p.Status=domain.StatusSucceeded
	if _,err:=payments.Create(context.Background(),p);err!=nil{t.Fatal(err)}
	refunds:=NewRefundRepository(db)
	now:=time.Date(2026,8,11,8,0,0,0,time.UTC)
	r,err:=domain.NewRefund("refund-1",p.ID,p.BookingID,"refund-key-1",10000,"usd",now);if err!=nil{t.Fatal(err)}
	if _,err=refunds.Create(context.Background(),r);err!=nil{t.Fatal(err)}
	conflict:=r;conflict.ID="refund-2"
	if _,err=refunds.Create(context.Background(),conflict);!errors.Is(err,repository.ErrRefundIdempotencyConflict){t.Fatalf("conflict err=%v",err)}
	a:=domain.RefundAttempt{ID:"refund-attempt-1",RefundID:r.ID,Outcome:domain.AttemptStarted,StartedAt:now.Add(time.Second)}
	if _,err=refunds.BeginAttempt(context.Background(),r.ID,a,a.StartedAt);err!=nil{t.Fatal(err)}
	unknown,err:=refunds.CompleteAttempt(context.Background(),r.ID,a.ID,domain.AttemptUnknown,domain.RefundUnknown,"request-1","","TIMEOUT","lost response",now.Add(2*time.Second));if err!=nil{t.Fatal(err)}
	if unknown.Status!=domain.RefundUnknown{t.Fatalf("status=%s",unknown.Status)}
	resolved,err:=refunds.ResolveUnknown(context.Background(),r.ID,domain.RefundSucceeded,"provider-refund-1","",now.Add(3*time.Second));if err!=nil{t.Fatal(err)}
	if resolved.Status!=domain.RefundSucceeded||resolved.ProviderReference!="provider-refund-1"{t.Fatalf("resolved=%+v",resolved)}
}
