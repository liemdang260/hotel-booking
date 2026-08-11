package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/repository"
)

type idemStore struct {
	record IdempotencyRecord
	claims int
}

func (s *idemStore) Claim(_ context.Context, c IdempotencyClaim) (IdempotencyRecord, error) {
	s.claims++
	if s.record.Key == "" {
		s.record = IdempotencyRecord{
			Key:         c.Key,
			RequestHash: c.RequestHash,
			BookingID:   c.ProposedBookingID,
			Status:      "PROCESSING",
		}
	}
	return s.record, nil
}

func TestSameIdempotencyRequestResumesExistingBooking(t *testing.T) {
	store := &idemStore{}
	g := NewIdempotencyGuard(store)
	in := CreateBookingIdentity{UserID: "u", QuoteID: "q", PaymentMethodRef: "pm", RoomQuantity: 1, GuestCount: 2}
	first, err := g.BeginOrResume(context.Background(), "key", "b1", in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.BeginOrResume(context.Background(), "key", "b2", in)
	if err != nil {
		t.Fatal(err)
	}
	if first.BookingID != "b1" || second.BookingID != "b1" || store.claims != 2 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestDifferentRequestForSameKeyIsRejected(t *testing.T) {
	store := &idemStore{}
	g := NewIdempotencyGuard(store)
	_, _ = g.BeginOrResume(context.Background(), "key", "b1", CreateBookingIdentity{UserID: "u", QuoteID: "q1"})
	_, err := g.BeginOrResume(context.Background(), "key", "b2", CreateBookingIdentity{UserID: "u", QuoteID: "q2"})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestFingerprintHasUnambiguousFieldBoundaries(t *testing.T) {
	a := CreateBookingIdentity{UserID: "ab", QuoteID: "c"}
	b := CreateBookingIdentity{UserID: "a", QuoteID: "bc"}
	if a.Hash() == b.Hash() {
		t.Fatal("ambiguous request fingerprint")
	}
}

func TestCreateBookingReturnsExistingBookingBeforeMutationSideEffects(t *testing.T) {
	var log []string
	quote := repository.Quote{
		ID:           "q1",
		HotelID:      "h1",
		RoomTypeID:   "rt1",
		Currency:     "USD",
		RoomQuantity: 1,
		GuestCount:   2,
		TotalMinor:   100,
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	identity := CreateBookingIdentity{
		UserID:           "u1",
		QuoteID:          "q1",
		PaymentMethodRef: "pm1",
		RoomQuantity:     quote.RoomQuantity,
		GuestCount:       quote.GuestCount,
	}
	guard := NewIdempotencyGuard(&idemStore{record: IdempotencyRecord{
		Key:         "key",
		RequestHash: identity.Hash(),
		BookingID:   "existing-booking",
		Status:      "PROCESSING",
	}})
	u := NewCreateBookingUsecase(
		pricingStub{quote: quote, log: &log},
		availabilityStub{log: &log},
		paymentStub{log: &log},
		stateStub{log: &log},
		guard,
	)
	u.now = func() time.Time { return time.Now() }

	out, err := u.Execute(context.Background(), CreateBookingInput{
		BookingID:        "new-booking",
		UserID:           "u1",
		QuoteID:          "q1",
		PaymentMethodRef: "pm1",
		IdempotencyKey:   "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.BookingID != "existing-booking" || out.Status != "PROCESSING" {
		t.Fatalf("out=%+v", out)
	}
	for _, entry := range log {
		if entry == "remote:reserve" || entry == "remote:payment" || entry == "remote:confirm" || entry == "persist:price" {
			t.Fatalf("idempotent resume executed side effect %q; log=%v", entry, log)
		}
	}
}
