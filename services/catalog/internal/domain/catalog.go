package domain

import (
	"errors"
	"strings"
)

var (
	ErrInvalidCatalogID = errors.New("invalid catalog id")
	ErrInvalidSearch    = errors.New("invalid catalog search")
)

type Hotel struct {
	ID          string
	Name        string
	Description string
	Address     string
	City        string
	Country     string
	Latitude    float64
	Longitude   float64
	Amenities   []string
	Active      bool
}

func (h Hotel) Valid() bool {
	return strings.TrimSpace(h.ID) != "" &&
		strings.TrimSpace(h.Name) != "" &&
		strings.TrimSpace(h.City) != "" &&
		strings.TrimSpace(h.Country) != ""
}

type RoomType struct {
	ID          string
	HotelID     string
	Name        string
	Description string
	Capacity    int32
	BedCount    int32
	Amenities   []string
	Active      bool
}

func (r RoomType) Valid() bool {
	return strings.TrimSpace(r.ID) != "" &&
		strings.TrimSpace(r.HotelID) != "" &&
		strings.TrimSpace(r.Name) != "" &&
		r.Capacity > 0 &&
		r.BedCount > 0
}

type Candidate struct {
	Hotel     Hotel
	RoomTypes []RoomType
}

type SearchFilter struct {
	City      string
	GuestCount int32
	Limit     int32
	PageToken string
}

type SearchResult struct {
	Candidates    []Candidate
	NextPageToken string
}
