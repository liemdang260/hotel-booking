package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain"
	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain/repository"
)

type catalogRepositoryStub struct {
	hotel domain.Hotel
	rooms []domain.RoomType
	search domain.SearchResult
	err error
	lastFilter domain.SearchFilter
}

func (s *catalogRepositoryStub) GetHotel(context.Context, string) (domain.Hotel, error) {
	if s.err != nil {
		return domain.Hotel{}, s.err
	}
	return s.hotel, nil
}

func (s *catalogRepositoryStub) GetRoomTypes(context.Context, string) ([]domain.RoomType, error) {
	return s.rooms, s.err
}

func (s *catalogRepositoryStub) SearchCandidates(_ context.Context, filter domain.SearchFilter) (domain.SearchResult, error) {
	s.lastFilter = filter
	return s.search, s.err
}

func TestSearchAppliesBoundedDefaultPageSize(t *testing.T) {
	repo := &catalogRepositoryStub{}
	service := NewCatalog(repo)
	if _, err := service.SearchCandidates(context.Background(), domain.SearchFilter{City: " Tokyo ", GuestCount: 2}); err != nil {
		t.Fatal(err)
	}
	if repo.lastFilter.City != "Tokyo" || repo.lastFilter.Limit != 20 {
		t.Fatalf("unexpected normalized filter: %+v", repo.lastFilter)
	}
}

func TestSearchRejectsUnboundedOrInvalidInput(t *testing.T) {
	service := NewCatalog(&catalogRepositoryStub{})
	cases := []domain.SearchFilter{
		{GuestCount: 0},
		{GuestCount: 17},
		{GuestCount: 2, Limit: 51},
		{GuestCount: 2, PageToken: string(make([]byte, 129))},
	}
	for _, filter := range cases {
		if _, err := service.SearchCandidates(context.Background(), filter); !errors.Is(err, domain.ErrInvalidSearch) {
			t.Fatalf("filter=%+v err=%v", filter, err)
		}
	}
}

func TestGetRoomTypesRejectsInactiveOrMissingHotel(t *testing.T) {
	service := NewCatalog(&catalogRepositoryStub{err: repository.ErrNotFound})
	if _, err := service.GetRoomTypes(context.Background(), "hotel-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}
