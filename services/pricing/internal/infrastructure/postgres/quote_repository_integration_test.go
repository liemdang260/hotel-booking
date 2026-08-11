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

	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
)

func openPricingIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PRICING_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PRICING_TEST_DATABASE_URL to a disposable migrated PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open pricing integration database: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping pricing integration database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestIntegrationQuoteRepositoryPersistsImmutableDateSnapshot(t *testing.T) {
	db := openPricingIntegrationDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE quotes`); err != nil {
		t.Fatalf("truncate quotes: %v", err)
	}

	checkIn, err := domain.NewDate(2026, time.September, 1)
	if err != nil {
		t.Fatal(err)
	}
	checkOut, err := domain.NewDate(2026, time.September, 4)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 2, 0, 0, 0, time.UTC)
	quote := domain.Quote{
		ID: "quote-integration-1",
		Input: domain.QuoteInput{
			HotelID: "hotel-1", RoomTypeID: "deluxe", CheckIn: checkIn, CheckOut: checkOut,
			GuestCount: 2, RoomQuantity: 2,
		},
		Price: domain.PriceBreakdown{
			SubtotalMinor: 60000, TaxMinor: 6000, ServiceFeeMinor: 500,
			DiscountMinor: 1000, TotalMinor: 65500,
		},
		Currency: "USD", PricingVersion: "v1", CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}

	repo := NewQuoteRepository(db)
	if err := repo.Insert(context.Background(), quote); err != nil {
		t.Fatalf("insert quote: %v", err)
	}
	got, err := repo.Get(context.Background(), quote.ID)
	if err != nil {
		t.Fatalf("get quote: %v", err)
	}
	if got.ID != quote.ID || got.Input.CheckIn != checkIn || got.Input.CheckOut != checkOut || got.Price.TotalMinor != 65500 || got.PricingVersion != "v1" {
		t.Fatalf("unexpected stored quote: %+v", got)
	}

	_, err = db.Exec(`UPDATE quotes SET total_minor = total_minor + 1 WHERE id = $1`, quote.ID)
	if err == nil {
		t.Fatal("immutable quote accepted UPDATE")
	}

	after, err := repo.Get(context.Background(), quote.ID)
	if err != nil {
		t.Fatalf("get quote after rejected update: %v", err)
	}
	if after.Price.TotalMinor != quote.Price.TotalMinor {
		t.Fatalf("quote changed after rejected update: got=%d want=%d", after.Price.TotalMinor, quote.Price.TotalMinor)
	}
}

func TestIntegrationQuoteSchemaRejectsInvalidStayAndTotals(t *testing.T) {
	db := openPricingIntegrationDB(t)
	if _, err := db.Exec(`TRUNCATE TABLE quotes`); err != nil {
		t.Fatalf("truncate quotes: %v", err)
	}
	now := time.Date(2026, 8, 11, 2, 0, 0, 0, time.UTC)
	_, err := db.Exec(`INSERT INTO quotes (
		id, hotel_id, room_type_id, check_in, check_out, guest_count, room_quantity,
		subtotal_minor, tax_minor, service_fee_minor, discount_minor, total_minor,
		currency, pricing_version, created_at, expires_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		"invalid-quote", "hotel-1", "deluxe", "2026-09-04", "2026-09-01", 2, 1,
		10000, 1000, 0, 0, 9999, "USD", "v1", now, now.Add(time.Minute),
	)
	if err == nil {
		t.Fatal("invalid stay/totals unexpectedly persisted")
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unexpected error type: %v", err)
	}
}
