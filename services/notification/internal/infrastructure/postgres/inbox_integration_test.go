//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/liemdang260/hotel-booking/services/notification/internal/domain/repository"
)

func TestIntegrationNotificationInboxDeduplicatesEventAndJob(t *testing.T) {
	db, err := sql.Open("postgres", os.Getenv("NOTIFICATION_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "TRUNCATE notification_jobs,notification_processed_events"); err != nil {
		t.Fatal(err)
	}
	inbox, err := NewInbox(db)
	if err != nil {
		t.Fatal(err)
	}
	event := repository.IntegrationEvent{
		ID: "70000000-0000-4000-8000-000000000001", Type: "BookingConfirmed",
		Version: 1, Payload: []byte{1, 2, 3}, CreatedAt: time.Unix(100, 0).UTC(),
	}
	job := repository.NotificationJob{EventID: event.ID, Kind: "BOOKING_CONFIRMATION", Payload: event.Payload}

	first, err := inbox.RecordEventAndJob(ctx, event, job)
	if err != nil || !first {
		t.Fatalf("first delivery: created=%v err=%v", first, err)
	}
	second, err := inbox.RecordEventAndJob(ctx, event, job)
	if err != nil || second {
		t.Fatalf("duplicate delivery: created=%v err=%v", second, err)
	}

	var processed, jobs int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM notification_processed_events").Scan(&processed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM notification_jobs").Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if processed != 1 || jobs != 1 {
		t.Fatalf("processed=%d jobs=%d", processed, jobs)
	}
}
