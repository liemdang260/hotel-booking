package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/repository"
)

type integrationPricing struct {
	quote repository.Quote
}

func (p integrationPricing) GetQuote(context.Context, string) (repository.Quote, error) {
	return p.quote, nil
}

type integrationAvailability struct {
	reserveErr       error
	confirmErr       error
	reserveCalls     int
	confirmCalls     int
	releaseCalls     int
	logicalReleases  map[string]struct{}
}

func (a *integrationAvailability) ReserveInventory(context.Context, repository.ReserveInventoryCommand) (repository.Reservation, error) {
	a.reserveCalls++
	return repository.Reservation{ID: "reservation-1", BookingID: "booking-1", Status: repository.ReservationHeld}, a.reserveErr
}

func (a *integrationAvailability) GetReservation(context.Context, string) (repository.Reservation, error) {
	return repository.Reservation{ID: "reservation-1", BookingID: "booking-1", Status: repository.ReservationHeld}, nil
}

func (a *integrationAvailability) ConfirmReservation(context.Context, string) (repository.Reservation, error) {
	a.confirmCalls++
	return repository.Reservation{ID: "reservation-1", BookingID: "booking-1", Status: repository.ReservationBooked}, a.confirmErr
}

func (a *integrationAvailability) ReleaseReservation(_ context.Context, reservationID, _ string) (repository.Reservation, error) {
	if a.logicalReleases == nil {
		a.logicalReleases = make(map[string]struct{})
	}
	if _, exists := a.logicalReleases[reservationID]; !exists {
		a.logicalReleases[reservationID] = struct{}{}
		a.releaseCalls++
	}
	return repository.Reservation{ID: reservationID, BookingID: "booking-1", Status: repository.ReservationReleased}, nil
}

type integrationPayment struct {
	payment repository.Payment
	err     error
	calls   int
}

func (p *integrationPayment) CreatePayment(context.Context, repository.CreatePaymentCommand) (repository.Payment, error) {
	p.calls++
	return p.payment, p.err
}

func (p *integrationPayment) GetPayment(context.Context, string) (repository.Payment, error) {
	return p.payment, p.err
}

type integrationSagaState struct {
	events             []string
	compensationReason string
}

func (s *integrationSagaState) add(event string) error {
	s.events = append(s.events, event)
	return nil
}

func (s *integrationSagaState) CreatePriceAccepted(context.Context, CreateBookingState) error {
	return s.add("PRICE_ACCEPTED")
}

func (s *integrationSagaState) MarkInventoryReserved(context.Context, string, repository.Reservation) error {
	return s.add("INVENTORY_RESERVED")
}

func (s *integrationSagaState) MarkPaymentRequested(context.Context, string, string) error {
	return s.add("PAYMENT_REQUESTED")
}

func (s *integrationSagaState) MarkPaymentUnknown(context.Context, string, error) error {
	return s.add("PAYMENT_UNKNOWN")
}

func (s *integrationSagaState) MarkPaymentFailedAndReleased(context.Context, string, string) error {
	return s.add("PAYMENT_FAILED_RELEASED")
}

func (s *integrationSagaState) MarkPaymentSucceeded(context.Context, string, repository.Payment) error {
	return s.add("PAYMENT_SUCCEEDED")
}

func (s *integrationSagaState) MarkConfirmationUnknown(context.Context, string, error) error {
	return s.add("CONFIRMATION_UNKNOWN")
}

func (s *integrationSagaState) MarkCompensationRequired(_ context.Context, _ string, reason string) error {
	s.compensationReason = reason
	return s.add("COMPENSATING")
}

func (s *integrationSagaState) MarkFailed(context.Context, string, string) error {
	return s.add("FAILED")
}

func (s *integrationSagaState) MarkConfirmed(context.Context, string, repository.Reservation) error {
	return s.add("CONFIRMED")
}

func newIntegrationSaga(availability *integrationAvailability, payment *integrationPayment, state *integrationSagaState) *CreateBookingUsecase {
	quote := repository.Quote{
		ID:           "quote-1",
		HotelID:      "hotel-1",
		RoomTypeID:   "room-type-1",
		Currency:     "USD",
		RoomQuantity: 1,
		TotalMinor:   25000,
		ExpiresAt:    time.Unix(2000, 0),
	}
	saga := NewCreateBookingUsecase(integrationPricing{quote: quote}, availability, payment, state)
	saga.now = func() time.Time { return time.Unix(1000, 0) }
	return saga
}

func integrationInput() CreateBookingInput {
	return CreateBookingInput{
		BookingID:       "booking-1",
		UserID:          "user-1",
		QuoteID:         "quote-1",
		PaymentMethodRef: "payment-method-1",
		IdempotencyKey:  "create-booking-1",
	}
}

func TestBookingSagaIntegrationHappyPathConfirms(t *testing.T) {
	availability := &integrationAvailability{}
	payment := &integrationPayment{payment: repository.Payment{ID: "payment-1", Status: repository.PaymentSucceeded}}
	state := &integrationSagaState{}

	output, err := newIntegrationSaga(availability, payment, state).Execute(context.Background(), integrationInput())
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if output.Status != "CONFIRMED" {
		t.Fatalf("status=%s", output.Status)
	}
	if availability.reserveCalls != 1 || payment.calls != 1 || availability.confirmCalls != 1 {
		t.Fatalf("reserve=%d payment=%d confirm=%d", availability.reserveCalls, payment.calls, availability.confirmCalls)
	}
	if state.events[len(state.events)-1] != "CONFIRMED" {
		t.Fatalf("events=%v", state.events)
	}
}

func TestBookingSagaIntegrationSoldOutNeverCharges(t *testing.T) {
	availability := &integrationAvailability{reserveErr: repository.ErrSoldOut}
	payment := &integrationPayment{}
	state := &integrationSagaState{}

	output, err := newIntegrationSaga(availability, payment, state).Execute(context.Background(), integrationInput())
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if output.Status != "FAILED" || payment.calls != 0 {
		t.Fatalf("status=%s payment calls=%d", output.Status, payment.calls)
	}
}

func TestBookingSagaIntegrationPaymentFailureReleasesHeldInventoryOnce(t *testing.T) {
	availability := &integrationAvailability{}
	payment := &integrationPayment{payment: repository.Payment{ID: "payment-1", Status: repository.PaymentFailed}}
	state := &integrationSagaState{}

	output, err := newIntegrationSaga(availability, payment, state).Execute(context.Background(), integrationInput())
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if output.Status != "PAYMENT_FAILED" {
		t.Fatalf("status=%s", output.Status)
	}
	if availability.releaseCalls != 1 {
		t.Fatalf("logical releases=%d", availability.releaseCalls)
	}
}

func TestBookingSagaIntegrationPaymentUnknownKeepsReservationHeld(t *testing.T) {
	availability := &integrationAvailability{}
	payment := &integrationPayment{err: repository.ErrOutcomeUnknown}
	state := &integrationSagaState{}

	output, err := newIntegrationSaga(availability, payment, state).Execute(context.Background(), integrationInput())
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if output.Status != "PAYMENT_UNKNOWN" {
		t.Fatalf("status=%s", output.Status)
	}
	if availability.releaseCalls != 0 || availability.confirmCalls != 0 {
		t.Fatalf("release=%d confirm=%d", availability.releaseCalls, availability.confirmCalls)
	}
	if state.events[len(state.events)-1] != "PAYMENT_UNKNOWN" {
		t.Fatalf("events=%v", state.events)
	}
}

func TestBookingSagaIntegrationExpiredReservationTriggersRefundCompensation(t *testing.T) {
	availability := &integrationAvailability{confirmErr: repository.ErrReservationExpired}
	payment := &integrationPayment{payment: repository.Payment{ID: "payment-1", Status: repository.PaymentSucceeded}}
	state := &integrationSagaState{}

	output, err := newIntegrationSaga(availability, payment, state).Execute(context.Background(), integrationInput())
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if output.Status != "COMPENSATING" {
		t.Fatalf("status=%s", output.Status)
	}
	if state.compensationReason != "REFUND_AFTER_RESERVATION_EXPIRED" {
		t.Fatalf("compensation=%s", state.compensationReason)
	}
	if availability.releaseCalls != 0 {
		t.Fatalf("a BOOKED/EXPIRED path must not use HELD release; releases=%d", availability.releaseCalls)
	}
}

type integrationRecoveryStore struct {
	saga      SagaSnapshot
	claimed   bool
	completed int
}

func (s *integrationRecoveryStore) ClaimDue(context.Context, time.Time, int, time.Duration) ([]SagaSnapshot, error) {
	if s.claimed {
		return nil, nil
	}
	s.claimed = true
	return []SagaSnapshot{s.saga}, nil
}

func (s *integrationRecoveryStore) MarkRecovered(context.Context, string, int64) error {
	s.completed++
	return nil
}

func (s *integrationRecoveryStore) ScheduleRetry(context.Context, string, int64, time.Time, string) error {
	return errors.New("unexpected retry")
}

type integrationResumer struct {
	calls int
	seen  SagaSnapshot
}

func (r *integrationResumer) Resume(_ context.Context, saga SagaSnapshot) error {
	r.calls++
	r.seen = saga
	return nil
}

func TestBookingSagaIntegrationRestartResumesPersistedIdentityOnce(t *testing.T) {
	store := &integrationRecoveryStore{saga: SagaSnapshot{
		ID:            "saga-1",
		BookingID:     "booking-1",
		State:         "PAYMENT_UNKNOWN",
		ReservationID: "reservation-1",
		PaymentID:     "payment-1",
		Version:       4,
	}}
	resumer := &integrationResumer{}
	recovery := NewRecoveryUsecase(store, resumer)

	processed, err := recovery.RecoverDue(context.Background(), 10)
	if err != nil || processed != 1 {
		t.Fatalf("processed=%d err=%v", processed, err)
	}
	processed, err = recovery.RecoverDue(context.Background(), 10)
	if err != nil || processed != 0 {
		t.Fatalf("second processed=%d err=%v", processed, err)
	}
	if resumer.calls != 1 || store.completed != 1 {
		t.Fatalf("resume calls=%d completed=%d", resumer.calls, store.completed)
	}
	if resumer.seen.BookingID != "booking-1" || resumer.seen.ReservationID != "reservation-1" || resumer.seen.PaymentID != "payment-1" {
		t.Fatalf("persisted identity not preserved: %+v", resumer.seen)
	}
}
