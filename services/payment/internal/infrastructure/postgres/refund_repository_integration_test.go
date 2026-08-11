//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/provider"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
	"github.com/liemdang260/hotel-booking/services/payment/internal/usecase"
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


type integrationRefundProvider struct{ result provider.RefundResult }

func (p integrationRefundProvider) Refund(context.Context, provider.RefundRequest) provider.RefundResult {
	return p.result
}
func (p integrationRefundProvider) GetRefund(context.Context, provider.RefundRequest) provider.RefundResult {
	return p.result
}

type integrationRefundIDs struct{ next int }

func (g *integrationRefundIDs) NewID() string {
	g.next++
	if g.next == 1 {
		return "refund-failed"
	}
	return "refund-attempt-failed"
}

type integrationRefundClock struct{ now time.Time }

func (c integrationRefundClock) Now() time.Time { return c.now }

func TestIntegrationRefundConfirmedProviderFailureIsDurable(t *testing.T) {
	db := openPaymentIntegrationDB(t)
	resetPaymentFixture(t, db)
	payments := NewPaymentRepository(db)
	payment := newIntegrationPayment(t, "payment-failed", "booking-failed", "payment-key-failed")
	payment.Status = domain.StatusSucceeded
	if _, err := payments.Create(context.Background(), payment); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	create := usecase.NewCreateRefund(
		NewRefundRepository(db),
		integrationRefundProvider{result: provider.RefundResult{
			Outcome: domain.AttemptDeclined, FailureCode: "REFUND_REJECTED", RawOutcome: "confirmed decline",
		}},
		&integrationRefundIDs{},
		integrationRefundClock{now: now},
	)
	refund, err := create.Execute(context.Background(), usecase.CreateRefundInput{
		PaymentID: payment.ID, BookingID: payment.BookingID, IdempotencyKey: "refund-key-failed",
		AmountMinor: 10000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refund.Status != domain.RefundFailed || refund.FailureCode != "REFUND_REJECTED" {
		t.Fatalf("refund=%+v", refund)
	}
	var attempts int
	if err := db.QueryRow(`SELECT count(*) FROM refund_attempts WHERE refund_id=$1 AND outcome='DECLINED'`, refund.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("declined attempts=%d, want 1", attempts)
	}
}
