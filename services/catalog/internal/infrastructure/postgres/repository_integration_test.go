//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain"
	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain/repository"
)

func openCatalogDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("CATALOG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CATALOG_TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`TRUNCATE catalog_room_types,catalog_hotels CASCADE`); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedCatalog(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
INSERT INTO catalog_hotels(id,name,description,address,city,country,latitude,longitude,amenities,active) VALUES
('00000000-0000-0000-0000-000000001001','Tokyo One','','1 Chiyoda','Tokyo','JP',35.68,139.76,ARRAY['wifi'],TRUE),
('00000000-0000-0000-0000-000000001002','Tokyo Two','','2 Chiyoda','Tokyo','JP',35.69,139.77,ARRAY['wifi','gym'],TRUE),
('00000000-0000-0000-0000-000000001003','Inactive','','3 Chiyoda','Tokyo','JP',35.70,139.78,ARRAY[]::TEXT[],FALSE);
INSERT INTO catalog_room_types(id,hotel_id,name,description,capacity,bed_count,amenities,active) VALUES
('00000000-0000-0000-0000-000000002001','00000000-0000-0000-0000-000000001001','Single','',1,1,ARRAY['desk'],TRUE),
('00000000-0000-0000-0000-000000002002','00000000-0000-0000-0000-000000001001','Double','',2,1,ARRAY['desk'],TRUE),
('00000000-0000-0000-0000-000000002003','00000000-0000-0000-0000-000000001002','Family','',4,2,ARRAY['crib'],TRUE),
('00000000-0000-0000-0000-000000002004','00000000-0000-0000-0000-000000001002','Hidden','',8,4,ARRAY[]::TEXT[],FALSE)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationSearchFilteringAndPagination(t *testing.T) {
	db := openCatalogDB(t)
	seedCatalog(t, db)
	repo := NewRepository(db)

	first, err := repo.SearchCandidates(context.Background(), domain.SearchFilter{City: "tokyo", GuestCount: 2, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Candidates) != 1 || first.Candidates[0].Hotel.Name != "Tokyo One" || len(first.Candidates[0].RoomTypes) != 1 || first.NextPageToken == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}

	second, err := repo.SearchCandidates(context.Background(), domain.SearchFilter{City: "Tokyo", GuestCount: 2, Limit: 1, PageToken: first.NextPageToken})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Candidates) != 1 || second.Candidates[0].Hotel.Name != "Tokyo Two" || second.NextPageToken != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}
}

func TestIntegrationInactiveAndMissingMetadataAreNotExposed(t *testing.T) {
	db := openCatalogDB(t)
	seedCatalog(t, db)
	repo := NewRepository(db)

	if _, err := repo.GetHotel(context.Background(), "00000000-0000-0000-0000-000000001003"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("inactive hotel err=%v", err)
	}
	if _, err := repo.GetHotel(context.Background(), "00000000-0000-0000-0000-000000009999"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing hotel err=%v", err)
	}
	rooms, err := repo.GetRoomTypes(context.Background(), "00000000-0000-0000-0000-000000001002")
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].Name != "Family" {
		t.Fatalf("inactive room type leaked: %+v", rooms)
	}
}
