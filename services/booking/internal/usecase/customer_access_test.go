package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

type customerReaderStub struct {
	booking domain.Booking
	listUser string
}

func (s *customerReaderStub) FindCustomerBooking(context.Context, string) (domain.Booking, error) {
	return s.booking, nil
}
func (s *customerReaderStub) ListCustomerBookings(_ context.Context, userID string, _ int, _ string) ([]domain.Booking, string, error) {
	s.listUser = userID
	return nil, "", nil
}

type creatorStub struct {
	input CreateBookingInput
}

func (s *creatorStub) Create(_ context.Context, input CreateBookingInput) (CreateBookingOutput, error) {
	s.input = input
	return CreateBookingOutput{BookingID: input.BookingID}, nil
}

type cancellerStub struct {
	called bool
}

func (s *cancellerStub) Cancel(context.Context, CancelBookingInput) (domain.BookingCancellation, error) {
	s.called = true
	return domain.BookingCancellation{}, nil
}

func TestCustomerAccessDerivesCreateOwnerFromPrincipal(t *testing.T) {
	reader := &customerReaderStub{}
	creator := &creatorStub{}
	service, err := NewCustomerAccess(reader, creator, &cancellerStub{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(context.Background(), CustomerPrincipal{UserID: "owner"}, "booking-1", "quote-1", "pm-1", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if creator.input.UserID != "owner" {
		t.Fatalf("create owner = %q", creator.input.UserID)
	}
}

func TestCustomerAccessRejectsCrossUserGetAndCancel(t *testing.T) {
	reader := &customerReaderStub{booking: domain.Booking{ID: "booking-1", UserID: "owner"}}
	canceller := &cancellerStub{}
	service, err := NewCustomerAccess(reader, &creatorStub{}, canceller)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Get(context.Background(), CustomerPrincipal{UserID: "attacker"}, "booking-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("get error = %v", err)
	}
	_, err = service.Cancel(context.Background(), CustomerPrincipal{UserID: "attacker"}, CancelBookingInput{
		BookingID: "booking-1", IdempotencyKey: "key", RequestHash: "hash", Reason: "CHANGE_OF_PLAN",
	})
	if !errors.Is(err, ErrForbidden) || canceller.called {
		t.Fatalf("cancel error = %v, called = %v", err, canceller.called)
	}
}

func TestCustomerAccessScopesListToPrincipal(t *testing.T) {
	reader := &customerReaderStub{}
	service, err := NewCustomerAccess(reader, &creatorStub{}, &cancellerStub{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.List(context.Background(), CustomerPrincipal{UserID: "owner"}, 20, ""); err != nil {
		t.Fatal(err)
	}
	if reader.listUser != "owner" {
		t.Fatalf("list user = %q", reader.listUser)
	}
}
