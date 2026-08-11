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

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/domain/repository"
)

func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("AVAILABILITY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("AVAILABILITY_TEST_DATABASE_URL is required for integration tests")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	return db
}

func resetInventoryFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
DELETE FROM reservation_inventory;
DELETE FROM reservations;
DELETE FROM availability_outbox_events;
DELETE FROM room_inventory;
INSERT INTO room_inventory (
    hotel_id, room_type_id, inventory_date,
    total_quantity, held_quantity, booked_quantity, version
) VALUES
('00000000-0000-0000-0000-000000000101', '00000000-0000-0000-0000-000000000102', DATE '2026-09-01', 10, 0, 0, 0),
('00000000-0000-0000-0000-000000000101', '00000000-0000-0000-0000-000000000102', DATE '2026-09-02', 10, 0, 0, 0);`)
	if err != nil {
		t.Fatalf("reset inventory fixture: %v", err)
	}
}

func TestIntegrationTransactionBoundaryCommitsAndRollsBack(t *testing.T) {
	db := openIntegrationDB(t)
	resetInventoryFixture(t, db)
	ctx := context.Background()
	transactor := NewTransactor(db)

	rollbackErr := errors.New("force rollback")
	err := transactor.WithinTransaction(ctx, func(ctx context.Context, repos repository.Repositories) error {
		items, err := repos.Inventory.LockRange(
			ctx,
			domain.HotelID("00000000-0000-0000-0000-000000000101"),
			domain.RoomTypeID("00000000-0000-0000-0000-000000000102"),
			time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
		)
		if err != nil {
			return err
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 locked inventory rows, got %d", len(items))
		}
		if !items[0].Date.Before(items[1].Date) {
			t.Fatal("inventory rows were not returned in stable date order")
		}
		items[0].HeldQuantity = 1
		if err := repos.Inventory.SaveInventory(ctx, &items[0]); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected rollback error, got %v", err)
	}

	var held, version int
	if err := db.QueryRow(`SELECT held_quantity, version FROM room_inventory WHERE inventory_date = DATE '2026-09-01'`).Scan(&held, &version); err != nil {
		t.Fatalf("read rolled-back inventory: %v", err)
	}
	if held != 0 || version != 0 {
		t.Fatalf("rollback leaked mutation: held=%d version=%d", held, version)
	}

	err = transactor.WithinTransaction(ctx, func(ctx context.Context, repos repository.Repositories) error {
		items, err := repos.Inventory.LockRange(
			ctx,
			domain.HotelID("00000000-0000-0000-0000-000000000101"),
			domain.RoomTypeID("00000000-0000-0000-0000-000000000102"),
			time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
		)
		if err != nil {
			return err
		}
		items[0].HeldQuantity = 2
		return repos.Inventory.SaveInventory(ctx, &items[0])
	})
	if err != nil {
		t.Fatalf("commit inventory update: %v", err)
	}

	if err := db.QueryRow(`SELECT held_quantity, version FROM room_inventory WHERE inventory_date = DATE '2026-09-01'`).Scan(&held, &version); err != nil {
		t.Fatalf("read committed inventory: %v", err)
	}
	if held != 2 || version != 1 {
		t.Fatalf("commit did not persist mutation: held=%d version=%d", held, version)
	}
}

func TestIntegrationSaveInventoryRejectsStaleVersion(t *testing.T) {
	db := openIntegrationDB(t)
	resetInventoryFixture(t, db)
	ctx := context.Background()
	store := NewStore(db)

	stale := &domain.Inventory{
		HotelID:        domain.HotelID("00000000-0000-0000-0000-000000000101"),
		RoomTypeID:     domain.RoomTypeID("00000000-0000-0000-0000-000000000102"),
		Date:           time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		TotalQuantity:  10,
		HeldQuantity:   1,
		BookedQuantity: 0,
		Version:        0,
	}
	fresh := *stale
	fresh.HeldQuantity = 2

	if err := store.SaveInventory(ctx, &fresh); err != nil {
		t.Fatalf("first optimistic update: %v", err)
	}
	if fresh.Version != 1 {
		t.Fatalf("expected returned version 1, got %d", fresh.Version)
	}

	err := store.SaveInventory(ctx, stale)
	if !errors.Is(err, repository.ErrConcurrentWrite) {
		t.Fatalf("expected ErrConcurrentWrite for stale version, got %v", err)
	}
}
