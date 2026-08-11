package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
)

type countingRates struct {
	calls int
	plan domain.RatePlan
}

func (r *countingRates) Current(context.Context, domain.QuoteInput) (domain.RatePlan, error) {
	r.calls++
	return r.plan, nil
}

func TestBatchEstimateCalculatesWithoutCreatingQuotes(t *testing.T) {
	rates := &countingRates{plan: domain.RatePlan{
		PricingVersion: "search-v1",
		Currency: "USD",
		NightlyMinor: 10000,
		TaxBasisPoints: 1000,
	}}
	usecase := NewBatchEstimateUsecase(rates)
	checkIn, _ := domain.NewDate(2026, time.September, 1)
	checkOut, _ := domain.NewDate(2026, time.September, 4)
	items := []EstimateInput{
		{HotelID: "h1", RoomTypeID: "r1", CheckIn: checkIn, CheckOut: checkOut, GuestCount: 2, RoomQuantity: 1},
		{HotelID: "h1", RoomTypeID: "r2", CheckIn: checkIn, CheckOut: checkOut, GuestCount: 2, RoomQuantity: 2},
	}
	results, err := usecase.Execute(context.Background(), items)
	if err != nil {
		t.Fatal(err)
	}
	if rates.calls != 2 || len(results) != 2 || results[0].TotalMinor != 33000 || results[1].TotalMinor != 66000 {
		t.Fatalf("calls=%d results=%+v", rates.calls, results)
	}
	if results[0].Currency != "USD" || results[0].PricingVersion != "search-v1" {
		t.Fatalf("result=%+v", results[0])
	}
}

func TestBatchEstimateRejectsUnboundedInput(t *testing.T) {
	rates := &countingRates{plan: domain.RatePlan{PricingVersion: "v1", Currency: "USD"}}
	usecase := NewBatchEstimateUsecase(rates)
	items := make([]EstimateInput, maxBatchEstimateItems+1)
	_, err := usecase.Execute(context.Background(), items)
	if !errors.Is(err, domain.ErrInvalidParty) || rates.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, rates.calls)
	}
}
