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

func TestIntegrationAvailabilityOutboxPublisherLeaseAndPublish(t *testing.T) {
	db, err := sql.Open("postgres", os.Getenv("AVAILABILITY_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "TRUNCATE availability_outbox_events"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO availability_outbox_events
		(id,aggregate_type,aggregate_id,aggregate_version,event_type,payload,status,available_at,created_at)
		VALUES('40000000-0000-4000-8000-000000000001','reservation','50000000-0000-4000-8000-000000000001',1,'ReservationExpired','{}','PENDING',$1,$1)`, time.Unix(100, 0).UTC()); err != nil {
		t.Fatal(err)
	}

	store := NewOutboxPublisherStore(db)
	now := time.Unix(200, 0).UTC()
	events, err := store.Claim(ctx, outbox.ClaimRequest{
		Limit: 10, ClaimToken: "60000000-0000-4000-8000-000000000001", Now: now, LeaseUntil: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].AggregateVersion != 1 {
		t.Fatalf("unexpected events: %#v", events)
	}
	if err := store.MarkPublished(ctx, events[0].ID, events[0].ClaimToken, now); err != nil {
		t.Fatal(err)
	}

	var status string
	var published time.Time
	if err := db.QueryRowContext(ctx, "SELECT status,published_at FROM availability_outbox_events WHERE id=$1", events[0].ID).Scan(&status, &published); err != nil {
		t.Fatal(err)
	}
	if status != "PUBLISHED" || !published.Equal(now) {
		t.Fatalf("status=%s published=%s", status, published)
	}
}

func TestIntegrationAvailabilityOutboxExpiredLeaseRejectsStaleFinalization(t *testing.T) {
	db, err := sql.Open("postgres", os.Getenv("AVAILABILITY_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "TRUNCATE availability_outbox_events"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO availability_outbox_events
		(id,aggregate_type,aggregate_id,aggregate_version,event_type,payload,status,available_at,created_at)
		VALUES('40000000-0000-4000-8000-000000000002','reservation','50000000-0000-4000-8000-000000000002',2,'ReservationExpired','{}','PENDING',$1,$1)`, time.Unix(100, 0).UTC()); err != nil {
		t.Fatal(err)
	}

	store := NewOutboxPublisherStore(db)
	firstNow := time.Unix(200, 0).UTC()
	firstToken := "60000000-0000-4000-8000-000000000002"
	first, err := store.Claim(ctx, outbox.ClaimRequest{
		Limit: 1, ClaimToken: firstToken, Now: firstNow, LeaseUntil: firstNow.Add(time.Minute),
	})
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: events=%#v err=%v", first, err)
	}

	beforeExpiry, err := store.Claim(ctx, outbox.ClaimRequest{
		Limit: 1, ClaimToken: "60000000-0000-4000-8000-000000000003", Now: firstNow.Add(30 * time.Second), LeaseUntil: firstNow.Add(90 * time.Second),
	})
	if err != nil || len(beforeExpiry) != 0 {
		t.Fatalf("claim before expiry: events=%#v err=%v", beforeExpiry, err)
	}

	secondNow := firstNow.Add(2 * time.Minute)
	secondToken := "60000000-0000-4000-8000-000000000004"
	second, err := store.Claim(ctx, outbox.ClaimRequest{
		Limit: 1, ClaimToken: secondToken, Now: secondNow, LeaseUntil: secondNow.Add(time.Minute),
	})
	if err != nil || len(second) != 1 || second[0].ClaimToken != secondToken {
		t.Fatalf("reclaim after expiry: events=%#v err=%v", second, err)
	}

	if err := store.MarkRetry(ctx, first[0].ID, firstToken, secondNow, "stale worker"); err == nil {
		t.Fatal("stale claim token unexpectedly finalized reclaimed event")
	}
	if err := store.MarkRetry(ctx, second[0].ID, secondToken, secondNow.Add(time.Second), "temporary Kafka failure"); err != nil {
		t.Fatal(err)
	}
}
