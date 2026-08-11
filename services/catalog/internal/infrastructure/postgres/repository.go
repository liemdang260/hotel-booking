package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/lib/pq"
	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain"
	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain/repository"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetHotel(ctx context.Context, id string) (domain.Hotel, error) {
	var hotel domain.Hotel
	err := r.db.QueryRowContext(ctx, `SELECT id::text,name,description,address,city,country,latitude,longitude,amenities,active
FROM catalog_hotels WHERE id=$1 AND active=TRUE`, id).Scan(
		&hotel.ID, &hotel.Name, &hotel.Description, &hotel.Address, &hotel.City, &hotel.Country,
		&hotel.Latitude, &hotel.Longitude, pq.Array(&hotel.Amenities), &hotel.Active,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Hotel{}, repository.ErrNotFound
	}
	if err != nil {
		return domain.Hotel{}, err
	}
	return hotel, nil
}

func (r *Repository) GetRoomTypes(ctx context.Context, hotelID string) ([]domain.RoomType, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,hotel_id::text,name,description,capacity,bed_count,amenities,active
FROM catalog_room_types WHERE hotel_id=$1 AND active=TRUE ORDER BY id`, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]domain.RoomType, 0)
	for rows.Next() {
		var room domain.RoomType
		if err := rows.Scan(&room.ID, &room.HotelID, &room.Name, &room.Description, &room.Capacity, &room.BedCount, pq.Array(&room.Amenities), &room.Active); err != nil {
			return nil, err
		}
		result = append(result, room)
	}
	return result, rows.Err()
}

func (r *Repository) SearchCandidates(ctx context.Context, filter domain.SearchFilter) (domain.SearchResult, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT h.id::text,h.name,h.description,h.address,h.city,h.country,h.latitude,h.longitude,h.amenities,h.active
FROM catalog_hotels h
WHERE h.active=TRUE
  AND ($1='' OR lower(h.city)=lower($1))
  AND ($2='' OR h.id>$2::uuid)
  AND EXISTS (
    SELECT 1 FROM catalog_room_types rt
    WHERE rt.hotel_id=h.id AND rt.active=TRUE AND rt.capacity >= $3
  )
ORDER BY h.id
LIMIT $4`, filter.City, filter.PageToken, filter.GuestCount, filter.Limit+1)
	if err != nil {
		return domain.SearchResult{}, err
	}

	hotels := make([]domain.Hotel, 0, filter.Limit+1)
	for rows.Next() {
		var hotel domain.Hotel
		if err := rows.Scan(&hotel.ID, &hotel.Name, &hotel.Description, &hotel.Address, &hotel.City, &hotel.Country, &hotel.Latitude, &hotel.Longitude, pq.Array(&hotel.Amenities), &hotel.Active); err != nil {
			rows.Close()
			return domain.SearchResult{}, err
		}
		hotels = append(hotels, hotel)
	}
	if err := rows.Close(); err != nil {
		return domain.SearchResult{}, err
	}
	if len(hotels) == 0 {
		return domain.SearchResult{Candidates: []domain.Candidate{}}, nil
	}

	result := domain.SearchResult{}
	if len(hotels) > int(filter.Limit) {
		hotels = hotels[:filter.Limit]
		result.NextPageToken = hotels[len(hotels)-1].ID
	}

	ids := make([]string, len(hotels))
	for i := range hotels {
		ids[i] = hotels[i].ID
	}
	roomRows, err := r.db.QueryContext(ctx, `SELECT id::text,hotel_id::text,name,description,capacity,bed_count,amenities,active
FROM catalog_room_types
WHERE hotel_id=ANY($1::uuid[]) AND active=TRUE AND capacity >= $2
ORDER BY hotel_id,id`, pq.Array(ids), filter.GuestCount)
	if err != nil {
		return domain.SearchResult{}, err
	}
	defer roomRows.Close()

	roomsByHotel := make(map[string][]domain.RoomType, len(hotels))
	for roomRows.Next() {
		var room domain.RoomType
		if err := roomRows.Scan(&room.ID, &room.HotelID, &room.Name, &room.Description, &room.Capacity, &room.BedCount, pq.Array(&room.Amenities), &room.Active); err != nil {
			return domain.SearchResult{}, err
		}
		roomsByHotel[room.HotelID] = append(roomsByHotel[room.HotelID], room)
	}
	if err := roomRows.Err(); err != nil {
		return domain.SearchResult{}, err
	}

	result.Candidates = make([]domain.Candidate, 0, len(hotels))
	for _, hotel := range hotels {
		result.Candidates = append(result.Candidates, domain.Candidate{Hotel: hotel, RoomTypes: roomsByHotel[hotel.ID]})
	}
	return result, nil
}

func NormalizeCity(city string) string {
	return strings.TrimSpace(city)
}
