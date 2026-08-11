//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/provider"
	"github.com/liemdang260/hotel-booking/services/payment/internal/usecase"
)

type integrationClock struct{ now time.Time }

func (c integrationClock) Now() time.Time { return c.now }

type integrationIDs struct{ next int }

func (g *integrationIDs) NewID() string {
	g.next++
	return "integration-payment-id-" + string(rune('0'+g.next))
}

type integrationChargeProvider struct {
	result provider.ChargeResult
	calls  int
}

func (p *integrationChargeProvider) Charge(context.Context, provider.ChargeRequest) provider.ChargeResult {
	p.calls++
	return p.result
}

type integrationLookupProvider struct{ result provider.LookupResult }

func (p integrationLookupProvider) GetPayment(context.Context, provider.LookupRequest) provider.LookupResult {
	return p.result
}

func TestIntegrationPaymentChargeRecoveryGate(t *testing.T) {
	db := openPaymentIntegrationDB(t)
	repo := NewPaymentRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 8, 11, 4, 0, 0, 0, time.UTC)

	newInput := func(bookingID, key string) usecase.CreatePaymentInput {
		return usecase.CreatePaymentInput{
			BookingID: bookingID, IdempotencyKey: key, AmountMinor: 25000,
			Currency: "usd", PaymentMethodRef: "pm-test",
		}
	}

	t.Run("success persists authoritative succeeded state", func(t *testing.T) {
		resetPaymentFixture(t, db)
		ids := &integrationIDs{}
		charge := &integrationChargeProvider{result: provider.ChargeResult{
			Outcome: domain.AttemptSucceeded, ProviderRequestRef: "req-success",
			ProviderReference: "provider-success", RawOutcome: `{"status":"succeeded"}`,
		}}
		create := usecase.NewCreatePayment(repo, charge, ids, integrationClock{now: now})

		payment, err := create.Execute(ctx, newInput("booking-success", "idem-success"))
		if err != nil {
			t.Fatalf("create payment: %v", err)
		}
		if payment.Status != domain.StatusSucceeded || payment.ProviderReference != "provider-success" {
			t.Fatalf("payment=%+v, want SUCCEEDED/provider-success", payment)
		}
		persisted, err := repo.GetByID(ctx, payment.ID)
		if err != nil {
			t.Fatalf("reload payment: %v", err)
		}
		if persisted.Status != domain.StatusSucceeded {
			t.Fatalf("persisted status=%s, want SUCCEEDED", persisted.Status)
		}
	})

	t.Run("definitive decline persists failed rather than unknown", func(t *testing.T) {
		resetPaymentFixture(t, db)
		ids := &integrationIDs{}
		charge := &integrationChargeProvider{result: provider.ChargeResult{
			Outcome: domain.AttemptDeclined, ProviderRequestRef: "req-decline",
			FailureCode: "CARD_DECLINED", RawOutcome: `{"status":"declined"}`,
		}}
		create := usecase.NewCreatePayment(repo, charge, ids, integrationClock{now: now})

		payment, err := create.Execute(ctx, newInput("booking-decline", "idem-decline"))
		if err != nil {
			t.Fatalf("create payment: %v", err)
		}
		if payment.Status != domain.StatusFailed || payment.FailureCode != "CARD_DECLINED" {
			t.Fatalf("payment=%+v, want FAILED/CARD_DECLINED", payment)
		}
	})

	t.Run("unknown is durable reconciled and replay does not recharge", func(t *testing.T) {
		resetPaymentFixture(t, db)
		ids := &integrationIDs{}
		charge := &integrationChargeProvider{result: provider.ChargeResult{
			Outcome: domain.AttemptUnknown, ProviderRequestRef: "req-unknown",
			ProviderReference: "provider-unknown", FailureCode: "TIMEOUT",
			RawOutcome: `{"status":"unknown"}`,
		}}
		clock := integrationClock{now: now}
		baseCreate := usecase.NewCreatePayment(repo, charge, ids, clock)
		create := usecase.NewCreatePaymentWithReconciliation(baseCreate, repo, clock, time.Second, 3)
		input := newInput("booking-unknown", "idem-unknown")

		payment, err := create.Execute(ctx, input)
		if err != nil {
			t.Fatalf("create unknown payment: %v", err)
		}
		if payment.Status != domain.StatusUnknown {
			t.Fatalf("status=%s, want UNKNOWN", payment.Status)
		}
		if charge.calls != 1 {
			t.Fatalf("charge calls=%d, want 1", charge.calls)
		}

		replay, err := create.Execute(ctx, input)
		if err != nil {
			t.Fatalf("replay payment: %v", err)
		}
		if replay.ID != payment.ID {
			t.Fatalf("replay id=%s, want %s", replay.ID, payment.ID)
		}
		if charge.calls != 1 {
			t.Fatalf("replay issued second charge: calls=%d", charge.calls)
		}

		jobs, err := repo.ClaimDue(ctx, now.Add(2*time.Second), now.Add(32*time.Second), 10)
		if err != nil {
			t.Fatalf("claim reconciliation: %v", err)
		}
		if len(jobs) != 1 {
			t.Fatalf("claimed jobs=%d, want 1", len(jobs))
		}

		reconcile := usecase.NewReconcilePayment(
			repo,
			integrationLookupProvider{result: provider.LookupResult{
				Outcome: domain.AttemptSucceeded, ProviderReference: "provider-final",
				RawOutcome: `{"status":"succeeded"}`,
			}},
			integrationClock{now: now.Add(3 * time.Second)},
			usecase.BackoffPolicy{Delays: []time.Duration{time.Second, 2 * time.Second}},
		)
		result, err := reconcile.Execute(ctx, jobs[0])
		if err != nil {
			t.Fatalf("reconcile payment: %v", err)
		}
		if !result.Resolved || result.Payment.Status != domain.StatusSucceeded {
			t.Fatalf("reconciliation result=%+v, want resolved SUCCEEDED", result)
		}

		persisted, err := repo.GetByID(ctx, payment.ID)
		if err != nil {
			t.Fatalf("reload reconciled payment: %v", err)
		}
		if persisted.Status != domain.StatusSucceeded || persisted.ProviderReference != "provider-final" {
			t.Fatalf("persisted=%+v, want SUCCEEDED/provider-final", persisted)
		}
	})
}
