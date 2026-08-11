package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
)

type testAuth struct {
	sawInternal bool
}

func (a *testAuth) Authenticate(_ context.Context, authorization string) (domain.Principal, error) {
	if authorization != "Bearer token" {
		return domain.Principal{}, domain.ErrUnauthenticated
	}
	return domain.Principal{UserID: "user-1", SubjectType: domain.SubjectUser}, nil
}

type testCustomerAPI struct {
	createInput domain.CreateBookingInput
	cancelInput domain.CancelBookingInput
}

func (a *testCustomerAPI) CreateQuote(context.Context, domain.Principal, domain.CreateQuoteInput) (domain.Quote, error) {
	return domain.Quote{ID: "quote-1", Currency: "USD", TotalMinor: 100, ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (a *testCustomerAPI) CreateBooking(_ context.Context, input domain.CreateBookingInput) (domain.Booking, error) {
	a.createInput = input
	return domain.Booking{ID: "booking-1", Status: "CONFIRMED", Currency: "USD"}, nil
}
func (a *testCustomerAPI) GetBooking(context.Context, domain.GetBookingInput) (domain.Booking, error) {
	return domain.Booking{ID: "booking-1"}, nil
}
func (a *testCustomerAPI) ListBookings(context.Context, domain.ListBookingsInput) (domain.BookingPage, error) {
	return domain.BookingPage{Bookings: []domain.Booking{}}, nil
}
func (a *testCustomerAPI) CancelBooking(_ context.Context, input domain.CancelBookingInput) (domain.CancellationResult, error) {
	a.cancelInput = input
	return domain.CancellationResult{Booking: domain.Booking{ID: input.BookingID}, CancellationID: "cancel-1", State: "STARTED"}, nil
}

func TestCreateBookingUsesVerifiedPrincipalAndUnchangedIdempotencyKey(t *testing.T) {
	auth := &testAuth{}
	api := &testCustomerAPI{}
	handler, err := NewCustomerHandler(auth, api, 4096, 8192)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bookings", strings.NewReader(`{"quote_id":"quote-1","payment_method_id":"pm-1"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Idempotency-Key", "idem-original")
	request.Header.Set("X-User-ID", "attacker")
	request.Header.Set("X-Roles", "admin")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if api.createInput.Actor.UserID != "user-1" || api.createInput.IdempotencyKey != "idem-original" {
		t.Fatalf("untrusted identity or changed key: %+v", api.createInput)
	}
	if request.Header.Get("X-User-ID") != "" || request.Header.Get("X-Roles") != "" {
		t.Fatal("client-controlled identity headers were not stripped")
	}
}

func TestCreateBookingRejectsMassAssignment(t *testing.T) {
	handler, err := NewCustomerHandler(&testAuth{}, &testCustomerAPI{}, 4096, 8192)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bookings", strings.NewReader(`{"quote_id":"quote-1","payment_method_id":"pm-1","user_id":"attacker","total_minor":1}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Idempotency-Key", "idem")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestCancelPropagatesCancellationAndDeadline(t *testing.T) {
	api := &testCustomerAPI{}
	handler, err := NewCustomerHandler(&testAuth{}, api, 4096, 8192)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bookings/booking-1/cancel", strings.NewReader(`{"reason":"CHANGE_OF_PLAN"}`)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Idempotency-Key", "cancel-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if api.cancelInput.BookingID != "booking-1" || api.cancelInput.IdempotencyKey != "cancel-key" {
		t.Fatalf("cancel input = %+v", api.cancelInput)
	}
	if api.cancelInput.Actor.UserID != "user-1" || request.Context().Err() != context.Canceled {
		t.Fatal("verified actor or upstream cancellation was not preserved")
	}
}
