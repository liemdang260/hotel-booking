package grpc

import (
	"context"
	"errors"

	catalogv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/catalog/v1"
	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain"
	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CatalogUsecases interface {
	GetHotel(context.Context, string) (domain.Hotel, error)
	GetRoomTypes(context.Context, string) ([]domain.RoomType, error)
	SearchCandidates(context.Context, domain.SearchFilter) (domain.SearchResult, error)
}

type Server struct {
	usecases CatalogUsecases
}

func NewServer(usecases CatalogUsecases) *Server {
	return &Server{usecases: usecases}
}

func (s *Server) GetHotel(ctx context.Context, request *catalogv1.GetHotelRequest) (*catalogv1.GetHotelResponse, error) {
	hotel, err := s.usecases.GetHotel(ctx, request.GetHotelId())
	if err != nil {
		return nil, mapError(err)
	}
	return &catalogv1.GetHotelResponse{Hotel: mapHotel(hotel)}, nil
}

func (s *Server) GetRoomTypes(ctx context.Context, request *catalogv1.GetRoomTypesRequest) (*catalogv1.GetRoomTypesResponse, error) {
	rooms, err := s.usecases.GetRoomTypes(ctx, request.GetHotelId())
	if err != nil {
		return nil, mapError(err)
	}
	response := &catalogv1.GetRoomTypesResponse{RoomTypes: make([]*catalogv1.RoomType, 0, len(rooms))}
	for _, room := range rooms {
		response.RoomTypes = append(response.RoomTypes, mapRoomType(room))
	}
	return response, nil
}

func (s *Server) SearchCandidates(ctx context.Context, request *catalogv1.SearchCandidatesRequest) (*catalogv1.SearchCandidatesResponse, error) {
	result, err := s.usecases.SearchCandidates(ctx, domain.SearchFilter{
		City:       request.GetCity(),
		GuestCount: request.GetGuestCount(),
		Limit:      request.GetPageSize(),
		PageToken:  request.GetPageToken(),
	})
	if err != nil {
		return nil, mapError(err)
	}
	response := &catalogv1.SearchCandidatesResponse{
		Candidates:   make([]*catalogv1.HotelCandidate, 0, len(result.Candidates)),
		NextPageToken: result.NextPageToken,
	}
	for _, candidate := range result.Candidates {
		item := &catalogv1.HotelCandidate{
			Hotel:     mapHotel(candidate.Hotel),
			RoomTypes: make([]*catalogv1.RoomType, 0, len(candidate.RoomTypes)),
		}
		for _, room := range candidate.RoomTypes {
			item.RoomTypes = append(item.RoomTypes, mapRoomType(room))
		}
		response.Candidates = append(response.Candidates, item)
	}
	return response, nil
}

func mapHotel(hotel domain.Hotel) *catalogv1.Hotel {
	return &catalogv1.Hotel{
		Id:          hotel.ID,
		Name:        hotel.Name,
		Description: hotel.Description,
		Address:     hotel.Address,
		City:        hotel.City,
		Country:     hotel.Country,
		Latitude:    hotel.Latitude,
		Longitude:   hotel.Longitude,
		Amenities:   append([]string(nil), hotel.Amenities...),
	}
}

func mapRoomType(room domain.RoomType) *catalogv1.RoomType {
	return &catalogv1.RoomType{
		Id:          room.ID,
		HotelId:     room.HotelID,
		Name:        room.Name,
		Description: room.Description,
		Capacity:    room.Capacity,
		BedCount:    room.BedCount,
		Amenities:   append([]string(nil), room.Amenities...),
	}
}

func mapError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "catalog operation canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "catalog operation deadline exceeded")
	case errors.Is(err, domain.ErrInvalidCatalogID), errors.Is(err, domain.ErrInvalidSearch):
		return status.Error(codes.InvalidArgument, "invalid catalog request")
	case errors.Is(err, repository.ErrNotFound):
		return status.Error(codes.NotFound, "catalog metadata not found")
	default:
		return status.Error(codes.Internal, "catalog operation failed")
	}
}
