package application

import (
	"context"

	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain"
)

type CatalogUsecases interface {
	GetHotel(context.Context, string) (domain.Hotel, error)
	GetRoomTypes(context.Context, string) ([]domain.RoomType, error)
	SearchCandidates(context.Context, domain.SearchFilter) (domain.SearchResult, error)
}

type Handler struct {
	catalog CatalogUsecases
}

func NewHandler(catalog CatalogUsecases) *Handler {
	return &Handler{catalog: catalog}
}

func (h *Handler) GetHotel(ctx context.Context, hotelID string) (domain.Hotel, error) {
	return h.catalog.GetHotel(ctx, hotelID)
}

func (h *Handler) GetRoomTypes(ctx context.Context, hotelID string) ([]domain.RoomType, error) {
	return h.catalog.GetRoomTypes(ctx, hotelID)
}

func (h *Handler) SearchCandidates(ctx context.Context, filter domain.SearchFilter) (domain.SearchResult, error) {
	return h.catalog.SearchCandidates(ctx, filter)
}
