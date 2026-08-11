package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
	"github.com/liemdang260/hotel-booking/services/pricing/internal/repository"
)

var ErrQuoteNotFound = repository.ErrQuoteNotFound

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewQuoteID() (string, error)
}

type CreateQuoteInput struct {
	HotelID      string
	RoomTypeID   string
	CheckIn      domain.Date
	CheckOut     domain.Date
	GuestCount   int
	RoomQuantity int
}

type CreateQuoteUsecase struct {
	quotes repository.QuoteRepository
	rates  repository.RatePlanRepository
	ids    IDGenerator
	clock  Clock
	ttl    time.Duration
}

func NewCreateQuoteUsecase(
	quotes repository.QuoteRepository,
	rates repository.RatePlanRepository,
	ids IDGenerator,
	clock Clock,
	ttl time.Duration,
) (*CreateQuoteUsecase, error) {
	if ttl <= 0 {
		return nil, errors.New("quote ttl must be positive")
	}
	return &CreateQuoteUsecase{quotes: quotes, rates: rates, ids: ids, clock: clock, ttl: ttl}, nil
}

func (u *CreateQuoteUsecase) Execute(ctx context.Context, input CreateQuoteInput) (domain.Quote, error) {
	quoteInput := domain.QuoteInput{
		HotelID: input.HotelID,
		RoomTypeID: input.RoomTypeID,
		CheckIn: input.CheckIn,
		CheckOut: input.CheckOut,
		GuestCount: input.GuestCount,
		RoomQuantity: input.RoomQuantity,
	}
	if err := quoteInput.Validate(); err != nil {
		return domain.Quote{}, err
	}
	plan, err := u.rates.Current(ctx, quoteInput)
	if err != nil {
		return domain.Quote{}, err
	}
	price, err := domain.CalculatePrice(quoteInput, plan)
	if err != nil {
		return domain.Quote{}, err
	}
	id, err := u.ids.NewQuoteID()
	if err != nil {
		return domain.Quote{}, err
	}
	now := u.clock.Now().UTC()
	quote := domain.Quote{
		ID: id,
		Input: quoteInput,
		Price: price,
		Currency: plan.Currency,
		PricingVersion: plan.PricingVersion,
		CreatedAt: now,
		ExpiresAt: now.Add(u.ttl),
	}
	if err := u.quotes.Insert(ctx, quote); err != nil {
		return domain.Quote{}, err
	}
	return quote, nil
}

type GetQuoteUsecase struct {
	quotes repository.QuoteRepository
	clock  Clock
}

func NewGetQuoteUsecase(quotes repository.QuoteRepository, clock Clock) *GetQuoteUsecase {
	return &GetQuoteUsecase{quotes: quotes, clock: clock}
}

func (u *GetQuoteUsecase) Execute(ctx context.Context, quoteID string) (domain.Quote, error) {
	if quoteID == "" {
		return domain.Quote{}, ErrQuoteNotFound
	}
	quote, err := u.quotes.Get(ctx, quoteID)
	if err != nil {
		return domain.Quote{}, err
	}
	if quote.IsExpired(u.clock.Now()) {
		return domain.Quote{}, domain.ErrQuoteExpired
	}
	return quote, nil
}
