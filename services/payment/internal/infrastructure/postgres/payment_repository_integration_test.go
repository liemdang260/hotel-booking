//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

func openPaymentIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PAYMENT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PAYMENT_TEST_DATABASE_URL to a disposable migrated PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open payment integration database: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping payment integration database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func resetPaymentFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE TABLE payment_attempts, payments CASCADE`); err != nil {
		t.Fatalf("reset payment fixture: %v", err)
	}
}

func newIntegrationPayment(t *testing.T, id, bookingID, key string) domain.Payment {
	t.Helper()
	p, err := domain.NewPayment(id, bookingID, key, 25000, "usd", "pm-1", time.Date(2026, 8, 11, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIntegrationPaymentUniquenessAndIdempotencyBoundaries(t *testing.T) {
	db := openPaymentIntegrationDB(t)
	resetPaymentFixture(t, db)
	repo := NewPaymentRepository(db)
	ctx := context.Background()

	first := newIntegrationPayment(t, "payment-1", "booking-1", "idem-1")
	if _, err := repo.Create(ctx, first); err != nil {
		t.Fatalf("create first payment: %v", err)
	}

	sameBooking := newIntegrationPayment(t, "payment-2", "booking-1", "idem-2")
	if _, err := repo.Create(ctx, sameBooking); !errors.Is(err, repository.ErrBookingConflict) {
		t.Fatalf("same booking err=%v, want booking conflict", err)
	}

	sameKey := newIntegrationPayment(t, "payment-3", "booking-3", "idem-1")
	if _, err := repo.Create(ctx, sameKey); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("same idempotency key err=%v, want idempotency conflict", err)
	}
}

func TestIntegrationPaymentAttemptTransitionIsAtomicAndAuditable(t *testing.T) {
	db := openPaymentIntegrationDB(t)
	resetPaymentFixture(t, db)
	repo := NewPaymentRepository(db)
	ctx := context.Background()

	p := newIntegrationPayment(t, "payment-1", "booking-1", "idem-1")
	if _, err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}

	startedAt := p.CreatedAt.Add(time.Second)
	attempt := domain.Attempt{
		ID: "attempt-1", PaymentID: p.ID, IdempotencyKey: "idem-1:attempt-1",
		ProviderRequestRef: "request-1", Outcome: domain.AttemptStarted, StartedAt: startedAt,
	}
	processing, err := repo.BeginAttempt(ctx, p.ID, attempt, startedAt)
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}
	if processing.Status != domain.StatusProcessing {
		t.Fatalf("status=%s, want PROCESSING", processing.Status)
	}

	finishedAt := startedAt.Add(time.Second)
	unknown, err := repo.CompleteAttempt(ctx, p.ID, attempt.ID, domain.AttemptUnknown, domain.StatusUnknown,
		"request-1", "provider-1", "TIMEOUT", `{"result":"unknown"}`, finishedAt)
	if err != nil {
		t.Fatalf("complete attempt: %v", err)
	}
	if unknown.Status != domain.StatusUnknown || unknown.ProviderReference != "provider-1" || unknown.FailureCode != "TIMEOUT" {
		t.Fatalf("unexpected completed payment: %+v", unknown)
	}

	var outcome, requestRef, providerRef, failure, raw string
	var finished sql.NullTime
	if err := db.QueryRow(`SELECT outcome, provider_request_ref, provider_reference, failure_code, raw_outcome, finished_at FROM payment_attempts WHERE id=$1`, attempt.ID).
		Scan(&outcome, &requestRef, &providerRef, &failure, &raw, &finished); err != nil {
		t.Fatalf("read attempt audit: %v", err)
	}
	if outcome != string(domain.AttemptUnknown) || requestRef != "request-1" || providerRef != "provider-1" || failure != "TIMEOUT" || raw == "" || !finished.Valid {
		t.Fatalf("attempt audit outcome=%s request=%s provider=%s failure=%s raw=%q finished=%v", outcome, requestRef, providerRef, failure, raw, finished.Valid)
	}
}
