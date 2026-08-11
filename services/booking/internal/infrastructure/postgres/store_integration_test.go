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

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

const (
	bookingID     = "00000000-0000-0000-0000-000000001001"
	sagaID        = "00000000-0000-0000-0000-000000001002"
	idempotencyID = "00000000-0000-0000-0000-000000001003"
	outboxID      = "00000000-0000-0000-0000-000000001004"
)

func openBookingIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("BOOKING_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("AVAILABILITY_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set BOOKING_TEST_DATABASE_URL to a disposable migrated PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open booking integration database: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping booking integration database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func resetBookingFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
TRUNCATE TABLE booking_outbox_events, booking_idempotency, booking_sagas, booking_price_snapshots, bookings CASCADE`); err != nil {
		t.Fatalf("reset booking fixture: %v", err)
	}
}

func bookingFixture(now time.Time) domain.Booking {
	return domain.Booking{
		ID:           bookingID,
		UserID:       "user-1",
		HotelID:      "hotel-1",
		RoomTypeID:   "room-type-1",
		CheckIn:      time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
		CheckOut:     time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		GuestCount:   2,
		RoomQuantity: 1,
		Status:       domain.BookingPending,
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func TestIntegrationBookingRepositoriesPersistAndReloadState(t *testing.T) {
	db := openBookingIntegrationDB(t)
	resetBookingFixture(t, db)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	transactor := Transactor{DB: db}

	err := transactor.WithinTransaction(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		booking := bookingFixture(now)
		if err := repos.Bookings.Create(ctx, &booking); err != nil {
			return err
		}
		snapshot := domain.PriceSnapshot{
			BookingID:       booking.ID,
			QuoteID:         "quote-1",
			Currency:        "USD",
			SubtotalMinor:   10000,
			TaxMinor:        1000,
			ServiceFeeMinor: 500,
			DiscountMinor:   500,
			TotalMinor:      11000,
			PricingVersion:  "pricing-v1",
			QuotedAt:        now.Add(-time.Minute),
			QuoteExpiresAt:  now.Add(time.Hour),
			AcceptedAt:      now,
		}
		if err := repos.PriceSnapshots.Create(ctx, &snapshot); err != nil {
			return err
		}
		saga := domain.BookingSaga{
			ID:        sagaID,
			BookingID: booking.ID,
			State:     domain.SagaPriceAccepted,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := repos.Sagas.Create(ctx, &saga); err != nil {
			return err
		}
		record := domain.IdempotencyRecord{
			ID:          idempotencyID,
			Key:         "idem-1",
			RequestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			BookingID:   booking.ID,
			Status:      domain.IdempotencyProcessing,
			CreatedAt:   now,
			UpdatedAt:   now,
			ExpiresAt:   now.Add(24 * time.Hour),
		}
		if err := repos.Idempotency.Claim(ctx, &record); err != nil {
			return err
		}
		event := domain.OutboxEvent{
			ID:            outboxID,
			AggregateType: "booking",
			AggregateID:   booking.ID,
			EventType:     "BookingCreated",
			EventVersion:  1,
			Payload:       []byte(`{"booking_id":"00000000-0000-0000-0000-000000001001"}`),
			Status:        domain.OutboxPending,
			CreatedAt:     now,
		}
		return repos.Outbox.Add(ctx, &event)
	})
	if err != nil {
		t.Fatalf("persist booking aggregate state: %v", err)
	}

	err = transactor.WithinTransaction(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		snapshot, err := repos.PriceSnapshots.FindByBookingID(ctx, bookingID)
		if err != nil {
			return err
		}
		if snapshot.QuoteID != "quote-1" || snapshot.TotalMinor != 11000 {
			t.Fatalf("unexpected price snapshot: %+v", snapshot)
		}

		saga, err := repos.Sagas.LockByBookingID(ctx, bookingID)
		if err != nil {
			return err
		}
		if saga.State != domain.SagaPriceAccepted {
			t.Fatalf("saga state=%s, want PRICE_ACCEPTED", saga.State)
		}
		if err := saga.Advance(domain.SagaReservingInventory); err != nil {
			return err
		}
		saga.UpdatedAt = now.Add(time.Minute)
		if err := repos.Sagas.Save(ctx, saga); err != nil {
			return err
		}

		record, err := repos.Idempotency.FindByKey(ctx, "idem-1")
		if err != nil {
			return err
		}
		if record.BookingID != bookingID || record.Status != domain.IdempotencyProcessing {
			t.Fatalf("unexpected idempotency record: %+v", record)
		}
		record.Status = domain.IdempotencyCompleted
		record.ResponsePayload = []byte(`{"booking_id":"00000000-0000-0000-0000-000000001001","status":"PENDING"}`)
		record.UpdatedAt = now.Add(time.Minute)
		return repos.Idempotency.Save(ctx, record)
	})
	if err != nil {
		t.Fatalf("reload/update booking repositories: %v", err)
	}

	var sagaState string
	var sagaVersion int64
	if err := db.QueryRow(`SELECT state, version FROM booking_sagas WHERE id=$1`, sagaID).Scan(&sagaState, &sagaVersion); err != nil {
		t.Fatalf("read saved saga: %v", err)
	}
	if sagaState != string(domain.SagaReservingInventory) || sagaVersion != 2 {
		t.Fatalf("saved saga state=%s version=%d", sagaState, sagaVersion)
	}

	var idemStatus string
	var response string
	if err := db.QueryRow(`SELECT status, response_payload::text FROM booking_idempotency WHERE id=$1`, idempotencyID).Scan(&idemStatus, &response); err != nil {
		t.Fatalf("read saved idempotency: %v", err)
	}
	if idemStatus != string(domain.IdempotencyCompleted) || response == "" {
		t.Fatalf("saved idempotency status=%s response=%q", idemStatus, response)
	}

	var outboxCount int
	if err := db.QueryRow(`SELECT count(*) FROM booking_outbox_events WHERE aggregate_id=$1`, bookingID).Scan(&outboxCount); err != nil {
		t.Fatalf("count booking outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("booking outbox count=%d, want 1", outboxCount)
	}
}

func TestIntegrationBookingTransactionRollsBackStateAndOutboxTogether(t *testing.T) {
	db := openBookingIntegrationDB(t)
	resetBookingFixture(t, db)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	transactor := Transactor{DB: db}
	sentinel := errors.New("rollback booking transaction")

	err := transactor.WithinTransaction(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		booking := bookingFixture(now)
		if err := repos.Bookings.Create(ctx, &booking); err != nil {
			return err
		}
		event := domain.OutboxEvent{
			ID:            outboxID,
			AggregateType: "booking",
			AggregateID:   booking.ID,
			EventType:     "BookingCreated",
			EventVersion:  1,
			Payload:       []byte(`{"booking_id":"00000000-0000-0000-0000-000000001001"}`),
			Status:        domain.OutboxPending,
			CreatedAt:     now,
		}
		if err := repos.Outbox.Add(ctx, &event); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error=%v, want sentinel", err)
	}

	var bookings, outbox int
	if err := db.QueryRow(`SELECT count(*) FROM bookings`).Scan(&bookings); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM booking_outbox_events`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if bookings != 0 || outbox != 0 {
		t.Fatalf("rollback leaked bookings=%d outbox=%d", bookings, outbox)
	}
}
