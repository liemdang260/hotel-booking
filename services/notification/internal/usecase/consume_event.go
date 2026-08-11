package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/notification/internal/domain/repository"
)

var ErrUnsupportedEvent = errors.New("notification: unsupported event")

type ConsumeEventInput struct {
	EventID   string
	EventType string
	Version   int
	Payload   []byte
	ReceivedAt time.Time
}

type ConsumeEventResult struct {
	Duplicate bool
}

type ConsumeEvent struct {
	inbox repository.Inbox
}

func NewConsumeEvent(inbox repository.Inbox) (*ConsumeEvent, error) {
	if inbox == nil {
		return nil, errors.New("notification: inbox is required")
	}
	return &ConsumeEvent{inbox: inbox}, nil
}

func (u *ConsumeEvent) Execute(ctx context.Context, input ConsumeEventInput) (ConsumeEventResult, error) {
	if input.EventID == "" || len(input.Payload) == 0 {
		return ConsumeEventResult{}, errors.New("notification: event id and payload are required")
	}

	kind, err := notificationKind(input.EventType, input.Version)
	if err != nil {
		return ConsumeEventResult{}, err
	}
	receivedAt := input.ReceivedAt.UTC()
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}

	created, err := u.inbox.RecordEventAndJob(ctx, repository.IntegrationEvent{
		ID: input.EventID, Type: input.EventType, Version: input.Version,
		Payload: append([]byte(nil), input.Payload...), CreatedAt: receivedAt,
	}, repository.NotificationJob{
		EventID: input.EventID, Kind: kind, Payload: append([]byte(nil), input.Payload...),
	})
	if err != nil {
		return ConsumeEventResult{}, fmt.Errorf("notification: persist consumed event: %w", err)
	}
	return ConsumeEventResult{Duplicate: !created}, nil
}

func notificationKind(eventType string, version int) (string, error) {
	if version != 1 {
		return "", fmt.Errorf("%w: %s v%d", ErrUnsupportedEvent, eventType, version)
	}
	switch eventType {
	case "BookingConfirmed":
		return "BOOKING_CONFIRMATION", nil
	case "ReservationExpired":
		return "RESERVATION_EXPIRATION", nil
	default:
		return "", fmt.Errorf("%w: %s v%d", ErrUnsupportedEvent, eventType, version)
	}
}
