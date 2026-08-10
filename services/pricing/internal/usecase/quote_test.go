package usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
)

type quoteMemory struct {
	items map[string]domain.Quote
}

func (m *quoteMemory) Insert(_ context.Context, quote domain.Quote) error {
	if m.items == nil {
		m.items = make(map[string]domain.Quote)
	}
	if _, exists := m.items[quote.ID]; exists {
		return errors.New("duplicate quote")
	}
	m.items[quote.ID] = quote
	return nil
}

func (m *quoteMemory) Get(_ context.Context, id string) (domain.Quote, error) {
	quote, exists := m.items[id]
	if !exists {
		return domain.Quote{}, ErrQuoteNotFound
	}
	return quote, nil
}

type rateMemory struct {
	plan domain.RatePlan
}

func (m *rateMemory) Current(context.Context, domain.QuoteInput) (domain.RatePlan, error) {
	return m.plan, nil
}

type sequenceIDs struct {
	next int
}

func (g *sequenceIDs) NewQuoteID() (string, error) {
	g.next++
	if g.next == 1 {
		return "quote-1", nil
	}
	return "quote-2", nil
}

type fixedClock struct {
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	return c.now
}

func mustDate(t *testing.T, year int, month time.Month, day int) domain.Date {
	t.Helper()
	date, err := domain.NewDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func quoteInput(t *testing.T) CreateQuoteInput {
	return CreateQuoteInput{
		HotelID: "hotel-1",
		RoomTypeID: "deluxe",
		CheckIn: mustDate(t, 2026, time.September, 1),
		CheckOut: mustDate(t, 2026, time.September, 4),
		GuestCount: 2,
		RoomQuantity: 2,
	}
}

func TestCreateQuoteUsesDateNightsAndIntegerMinorUnits(t *testing.T) {
	store := &quoteMemory{}
	rates := &rateMemory{plan: domain.RatePlan{
		PricingVersion: "v1",
		Currency: "USD",
		NightlyMinor: 10000,
		TaxBasisPoints: 1000,
		ServiceFeeMinor: 500,
		DiscountMinor: 1000,
	}}
	clock := &fixedClock{now: time.Date(2026, 8, 1, 23, 30, 0, 0, time.FixedZone("client", 7*60*60))}
	usecase, err := NewCreateQuoteUsecase(store, rates, &sequenceIDs{}, clock, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	quote, err := usecase.Execute(context.Background(), quoteInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if quote.Price.SubtotalMinor != 60000 || quote.Price.TaxMinor != 6000 || quote.Price.TotalMinor != 65500 {
		t.Fatalf("price=%+v", quote.Price)
	}
	if quote.CreatedAt.Location() != time.UTC || quote.ExpiresAt.Sub(quote.CreatedAt) != 5*time.Minute {
		t.Fatalf("created=%v expires=%v", quote.CreatedAt, quote.ExpiresAt)
	}
}

func TestGetQuoteReturnsImmutableStoredSnapshot(t *testing.T) {
	store := &quoteMemory{}
	rates := &rateMemory{plan: domain.RatePlan{PricingVersion: "v1", Currency: "USD", NightlyMinor: 10000}}
	clock := &fixedClock{now: time.Unix(1000, 0)}
	create, _ := NewCreateQuoteUsecase(store, rates, &sequenceIDs{}, clock, time.Hour)
	created, err := create.Execute(context.Background(), quoteInput(t))
	if err != nil {
		t.Fatal(err)
	}

	rates.plan.NightlyMinor = 20000
	got, err := NewGetQuoteUsecase(store, clock).Execute(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, created) {
		t.Fatalf("stored quote mutated: got=%+v want=%+v", got, created)
	}
}

func TestChangedPricingCreatesNewQuote(t *testing.T) {
	store := &quoteMemory{}
	rates := &rateMemory{plan: domain.RatePlan{PricingVersion: "v1", Currency: "USD", NightlyMinor: 10000}}
	ids := &sequenceIDs{}
	clock := &fixedClock{now: time.Unix(1000, 0)}
	create, _ := NewCreateQuoteUsecase(store, rates, ids, clock, time.Hour)

	first, err := create.Execute(context.Background(), quoteInput(t))
	if err != nil {
		t.Fatal(err)
	}
	rates.plan.PricingVersion = "v2"
	rates.plan.NightlyMinor = 12000
	second, err := create.Execute(context.Background(), quoteInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.PricingVersion == second.PricingVersion || first.Price.TotalMinor == second.Price.TotalMinor {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if store.items[first.ID].PricingVersion != "v1" {
		t.Fatal("first accepted quote was changed")
	}
}

func TestExpiredQuoteIsStructuredOutcome(t *testing.T) {
	store := &quoteMemory{}
	rates := &rateMemory{plan: domain.RatePlan{PricingVersion: "v1", Currency: "USD", NightlyMinor: 10000}}
	clock := &fixedClock{now: time.Unix(1000, 0)}
	create, _ := NewCreateQuoteUsecase(store, rates, &sequenceIDs{}, clock, time.Minute)
	created, err := create.Execute(context.Background(), quoteInput(t))
	if err != nil {
		t.Fatal(err)
	}

	clock.now = created.ExpiresAt
	_, err = NewGetQuoteUsecase(store, clock).Execute(context.Background(), created.ID)
	if !errors.Is(err, domain.ErrQuoteExpired) {
		t.Fatalf("err=%v", err)
	}
}

func TestInvalidDateRangeIsRejectedBeforePersistence(t *testing.T) {
	store := &quoteMemory{}
	rates := &rateMemory{plan: domain.RatePlan{PricingVersion: "v1", Currency: "USD", NightlyMinor: 10000}}
	create, _ := NewCreateQuoteUsecase(store, rates, &sequenceIDs{}, &fixedClock{now: time.Unix(1000, 0)}, time.Minute)
	input := quoteInput(t)
	input.CheckOut = input.CheckIn

	_, err := create.Execute(context.Background(), input)
	if !errors.Is(err, domain.ErrInvalidStay) {
		t.Fatalf("err=%v", err)
	}
	if len(store.items) != 0 {
		t.Fatal("invalid quote was persisted")
	}
}
