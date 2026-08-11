//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/liemdang260/hotel-booking/internal/outbox"
)

func TestIntegrationBookingOutboxPublisherLeaseAndRetry(t *testing.T) {
	db, err := sql.Open("postgres", os.Getenv("BOOKING_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "TRUNCATE booking_outbox_events"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO booking_outbox_events
		(id,aggregate_type,aggregate_id,event_type,event_version,payload,status,created_at)
		VALUES('10000000-0000-4000-8000-000000000001','booking','20000000-0000-4000-8000-000000000001','BookingConfirmed',1,'{}','PENDING',$1)`, time.Unix(100, 0).UTC()); err != nil {
		t.Fatal(err)
	}

	store := NewOutboxPublisherStore(db)
	now := time.Unix(200, 0).UTC()
	events, err := store.Claim(ctx, outbox.ClaimRequest{
		Limit: 10, ClaimToken: "30000000-0000-4000-8000-000000000001", Now: now, LeaseUntil: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Attempt != 0 {
		t.Fatalf("unexpected events: %#v", events)
	}
	if err := store.MarkRetry(ctx, events[0].ID, events[0].ClaimToken, now.Add(time.Second), "kafka unavailable"); err != nil {
		t.Fatal(err)
	}

	var status string
	var retries int
	if err := db.QueryRowContext(ctx, "SELECT status,retry_count FROM booking_outbox_events WHERE id=$1", events[0].ID).Scan(&status, &retries); err != nil {
		t.Fatal(err)
	}
	if status != "PENDING" || retries != 1 {
		t.Fatalf("status=%s retries=%d", status, retries)
	}
}

func TestIntegrationBookingOutboxExpiredLeaseRejectsStaleFinalization(t *testing.T) {
	db, err := sql.Open("postgres", os.Getenv("BOOKING_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "TRUNCATE booking_outbox_events"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO booking_outbox_events
		(id,aggregate_type,aggregate_id,event_type,event_version,payload,status,created_at)
		VALUES('10000000-0000-4000-8000-000000000002','booking','20000000-0000-4000-8000-000000000002','BookingConfirmed',1,'{}','PENDING',$1)`, time.Unix(100, 0).UTC()); err != nil {
		t.Fatal(err)
	}

	store := NewOutboxPublisherStore(db)
	firstNow := time.Unix(200, 0).UTC()
	firstToken := "30000000-0000-4000-8000-000000000002"
	first, err := store.Claim(ctx, outbox.ClaimRequest{
		Limit: 1, ClaimToken: firstToken, Now: firstNow, LeaseUntil: firstNow.Add(time.Minute),
	})
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: events=%#v err=%v", first, err)
	}

	beforeExpiry, err := store.Claim(ctx, outbox.ClaimRequest{
		Limit: 1, ClaimToken: "30000000-0000-4000-8000-000000000003", Now: firstNow.Add(30 * time.Second), LeaseUntil: firstNow.Add(90 * time.Second),
	})
	if err != nil || len(beforeExpiry) != 0 {
		t.Fatalf("claim before expiry: events=%#v err=%v", beforeExpiry, err)
	}

	secondNow := firstNow.Add(2 * time.Minute)
	secondToken := "30000000-0000-4000-8000-000000000004"
	second, err := store.Claim(ctx, outbox.ClaimRequest{
		Limit: 1, ClaimToken: secondToken, Now: secondNow, LeaseUntil: secondNow.Add(time.Minute),
	})
	if err != nil || len(second) != 1 || second[0].ClaimToken != secondToken {
		t.Fatalf("reclaim after expiry: events=%#v err=%v", second, err)
	}

	if err := store.MarkPublished(ctx, first[0].ID, firstToken, secondNow); err == nil {
		t.Fatal("stale claim token unexpectedly finalized reclaimed event")
	}
	if err := store.MarkPublished(ctx, second[0].ID, secondToken, secondNow); err != nil {
		t.Fatal(err)
	}
}
