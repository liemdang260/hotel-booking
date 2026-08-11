package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/notification/internal/domain/repository"
)

type memoryInbox struct {
	seen map[string]struct{}
	jobs []repository.NotificationJob
}

func (m *memoryInbox) RecordEventAndJob(_ context.Context, event repository.IntegrationEvent, job repository.NotificationJob) (bool, error) {
	if _, ok := m.seen[event.ID]; ok {
		return false, nil
	}
	m.seen[event.ID] = struct{}{}
	m.jobs = append(m.jobs, job)
	return true, nil
}

func TestConsumeEventCreatesOneJobForDuplicateDelivery(t *testing.T) {
	inbox := &memoryInbox{seen: make(map[string]struct{})}
	consume, err := NewConsumeEvent(inbox)
	if err != nil {
		t.Fatal(err)
	}
	input := ConsumeEventInput{
		EventID: "10000000-0000-4000-8000-000000000001", EventType: "BookingConfirmed",
		Version: 1, Payload: []byte{1, 2, 3}, ReceivedAt: time.Unix(100, 0),
	}
	first, err := consume.Execute(context.Background(), input)
	if err != nil || first.Duplicate {
		t.Fatalf("first delivery: result=%#v err=%v", first, err)
	}
	second, err := consume.Execute(context.Background(), input)
	if err != nil || !second.Duplicate {
		t.Fatalf("duplicate delivery: result=%#v err=%v", second, err)
	}
	if len(inbox.jobs) != 1 || inbox.jobs[0].Kind != "BOOKING_CONFIRMATION" {
		t.Fatalf("unexpected jobs: %#v", inbox.jobs)
	}
}

func TestConsumeEventRejectsUnsupportedVersion(t *testing.T) {
	consume, err := NewConsumeEvent(&memoryInbox{seen: make(map[string]struct{})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := consume.Execute(context.Background(), ConsumeEventInput{
		EventID: "event", EventType: "ReservationExpired", Version: 2, Payload: []byte{1},
	}); err == nil {
		t.Fatal("expected unsupported version error")
	}
}
