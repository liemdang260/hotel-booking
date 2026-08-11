package repository

import (
	"context"
	"errors"

	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain"
)

var ErrNotFound = errors.New("catalog metadata not found")

type Catalog interface {
	GetHotel(context.Context, string) (domain.Hotel, error)
	GetRoomTypes(context.Context, string) ([]domain.RoomType, error)
	SearchCandidates(context.Context, domain.SearchFilter) (domain.SearchResult, error)
}
