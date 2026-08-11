package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain/repository"
)

type catalogStub struct {
	result domain.CatalogSearchResult
	err error
	calls int
	last repository.CatalogSearchInput
}
func (s *catalogStub) SearchCandidates(_ context.Context, input repository.CatalogSearchInput) (domain.CatalogSearchResult, error) {
	s.calls++
	s.last = input
	return s.result, s.err
}

type availabilityStub struct {
	results []domain.Availability
	err error
	calls int
	items []repository.AvailabilityItem
	sawDeadline bool
}
func (s *availabilityStub) BatchCheck(ctx context.Context, items []repository.AvailabilityItem) ([]domain.Availability, error) {
	s.calls++
	s.items = append([]repository.AvailabilityItem(nil), items...)
	_, s.sawDeadline = ctx.Deadline()
	return s.results, s.err
}

type pricingStub struct {
	results []domain.PriceEstimate
	err error
	calls int
	items []repository.PricingItem
	sawDeadline bool
}
func (s *pricingStub) BatchEstimate(ctx context.Context, items []repository.PricingItem) ([]domain.PriceEstimate, error) {
	s.calls++
	s.items = append([]repository.PricingItem(nil), items...)
	_, s.sawDeadline = ctx.Deadline()
	return s.results, s.err
}

func searchFixture(t *testing.T) (*SearchHotels, *catalogStub, *availabilityStub, *pricingStub, domain.SearchInput) {
	t.Helper()
	catalog := &catalogStub{result: domain.CatalogSearchResult{
		Candidates: []domain.CatalogCandidate{{
			Hotel: domain.Hotel{ID: "h1", Name: "Hotel"},
			RoomTypes: []domain.RoomType{
				{ID: "r1", HotelID: "h1", Name: "Queen"},
				{ID: "r2", HotelID: "h1", Name: "Twin"},
			},
		}},
		NextPageToken: "next",
	}}
	availability := &availabilityStub{results: []domain.Availability{
		{HotelID: "h1", RoomTypeID: "r1", Available: true, AvailableQuantity: 2},
		{HotelID: "h1", RoomTypeID: "r2", Available: false, AvailableQuantity: 0},
	}}
	pricing := &pricingStub{results: []domain.PriceEstimate{
		{HotelID: "h1", RoomTypeID: "r1", TotalMinor: 30000, Currency: "USD", PricingVersion: "v1"},
		{HotelID: "h1", RoomTypeID: "r2", TotalMinor: 25000, Currency: "USD", PricingVersion: "v1"},
	}}
	service, err := NewSearchHotels(catalog, availability, pricing, SearchConfig{
		DefaultPageSize: 20,
		MaxCandidates: 50,
		MaxBatchItems: 100,
		DownstreamTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := domain.SearchInput{
		City: " Tokyo ",
		CheckIn: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		CheckOut: time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC),
		GuestCount: 2,
		RoomQuantity: 1,
	}
	return service, catalog, availability, pricing, input
}

func TestSearchUsesOneBoundedBatchCallPerDependency(t *testing.T) {
	service, catalog, availability, pricing, input := searchFixture(t)
	result, err := service.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.calls != 1 || catalog.last.City != "Tokyo" || catalog.last.PageSize != 20 {
		t.Fatalf("catalog calls=%d input=%+v", catalog.calls, catalog.last)
	}
	if availability.calls != 1 || pricing.calls != 1 || len(availability.items) != 2 || len(pricing.items) != 2 {
		t.Fatalf("availability calls/items=%d/%d pricing calls/items=%d/%d", availability.calls, len(availability.items), pricing.calls, len(pricing.items))
	}
	if !availability.sawDeadline || !pricing.sawDeadline {
		t.Fatal("downstream calls did not receive a bounded deadline")
	}
	if len(result.Hotels) != 1 || len(result.Hotels[0].Rooms) != 2 || !result.Advisory || result.NextPageToken != "next" {
		t.Fatalf("result=%+v", result)
	}
}

func TestSearchFailsRatherThanReturningUndocumentedPartialData(t *testing.T) {
	service, _, availability, _, input := searchFixture(t)
	availability.results = availability.results[:1]
	_, err := service.Execute(context.Background(), input)
	if !errors.Is(err, domain.ErrDependencyIncomplete) {
		t.Fatalf("err=%v", err)
	}
}

func TestSearchStopsBeforeFanoutWhenCatalogFails(t *testing.T) {
	service, catalog, availability, pricing, input := searchFixture(t)
	catalog.err = errors.New("catalog unavailable")
	_, err := service.Execute(context.Background(), input)
	if err == nil || availability.calls != 0 || pricing.calls != 0 {
		t.Fatalf("err=%v availability=%d pricing=%d", err, availability.calls, pricing.calls)
	}
}

func TestSearchRejectsBatchLargerThanConfiguredBound(t *testing.T) {
	service, catalog, _, _, input := searchFixture(t)
	rooms := make([]domain.RoomType, 101)
	for i := range rooms {
		rooms[i] = domain.RoomType{ID: string(rune(i + 1)), HotelID: "h1"}
	}
	catalog.result.Candidates[0].RoomTypes = rooms
	_, err := service.Execute(context.Background(), input)
	if !errors.Is(err, domain.ErrInvalidSearch) {
		t.Fatalf("err=%v", err)
	}
}
