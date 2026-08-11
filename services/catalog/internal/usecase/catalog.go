package usecase

import (
	"context"
	"strings"

	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain"
	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain/repository"
)

const (
	defaultPageSize int32 = 20
	maxPageSize     int32 = 50
	maxGuestCount   int32 = 16
	maxTokenLength        = 128
)

type Catalog struct {
	repository repository.Catalog
}

func NewCatalog(r repository.Catalog) *Catalog {
	return &Catalog{repository: r}
}

func (c *Catalog) GetHotel(ctx context.Context, id string) (domain.Hotel, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.Hotel{}, domain.ErrInvalidCatalogID
	}
	return c.repository.GetHotel(ctx, id)
}

func (c *Catalog) GetRoomTypes(ctx context.Context, hotelID string) ([]domain.RoomType, error) {
	hotelID = strings.TrimSpace(hotelID)
	if hotelID == "" {
		return nil, domain.ErrInvalidCatalogID
	}
	if _, err := c.repository.GetHotel(ctx, hotelID); err != nil {
		return nil, err
	}
	return c.repository.GetRoomTypes(ctx, hotelID)
}

func (c *Catalog) SearchCandidates(ctx context.Context, filter domain.SearchFilter) (domain.SearchResult, error) {
	filter.City = strings.TrimSpace(filter.City)
	filter.PageToken = strings.TrimSpace(filter.PageToken)
	if filter.GuestCount <= 0 || filter.GuestCount > maxGuestCount || len(filter.PageToken) > maxTokenLength {
		return domain.SearchResult{}, domain.ErrInvalidSearch
	}
	if filter.Limit == 0 {
		filter.Limit = defaultPageSize
	}
	if filter.Limit < 0 || filter.Limit > maxPageSize {
		return domain.SearchResult{}, domain.ErrInvalidSearch
	}
	return c.repository.SearchCandidates(ctx, filter)
}
