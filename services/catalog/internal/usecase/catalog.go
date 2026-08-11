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
)

type Catalog struct {
	repository repository.Catalog
}

func NewCatalog(r repository.Catalog) *Catalog {
	return &Catalog{repository: r}
}

func (c *Catalog) GetHotel(ctx context.Context, id string) (domain.Hotel, error) {
	id = strings.TrimSpace(id)
	if !validUUID(id) {
		return domain.Hotel{}, domain.ErrInvalidCatalogID
	}
	return c.repository.GetHotel(ctx, id)
}

func (c *Catalog) GetRoomTypes(ctx context.Context, hotelID string) ([]domain.RoomType, error) {
	hotelID = strings.TrimSpace(hotelID)
	if !validUUID(hotelID) {
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
	if filter.GuestCount <= 0 || filter.GuestCount > maxGuestCount {
		return domain.SearchResult{}, domain.ErrInvalidSearch
	}
	if filter.PageToken != "" && !validUUID(filter.PageToken) {
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

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for i, char := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}
