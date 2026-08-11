package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	availabilityeventsv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/events/availability/v1"
	bookingeventsv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/events/booking/v1"
	eventsv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/events/common/v1"
	"github.com/liemdang260/hotel-booking/services/notification/internal/usecase"
	"google.golang.org/protobuf/proto"
)

type EventUsecase interface {
	Execute(context.Context, usecase.ConsumeEventInput) (usecase.ConsumeEventResult, error)
}

type KafkaConsumer struct {
	consume EventUsecase
	clock   func() time.Time
}

func NewKafkaConsumer(consume EventUsecase, clock func() time.Time) (*KafkaConsumer, error) {
	if consume == nil || clock == nil {
		return nil, errors.New("notification consumer: usecase and clock are required")
	}
	return &KafkaConsumer{consume: consume, clock: clock}, nil
}

func (c *KafkaConsumer) Handle(ctx context.Context, value []byte) error {
	var envelope eventsv1.EventEnvelope
	if err := proto.Unmarshal(value, &envelope); err != nil {
		return fmt.Errorf("notification consumer: decode envelope: %w", err)
	}
	if envelope.EventId == "" || envelope.EventType == "" || envelope.EventVersion == 0 || len(envelope.Payload) == 0 {
		return errors.New("notification consumer: incomplete envelope")
	}
	if err := validatePayload(envelope.EventType, envelope.EventVersion, envelope.Payload); err != nil {
		return fmt.Errorf("notification consumer: validate payload: %w", err)
	}
	_, err := c.consume.Execute(ctx, usecase.ConsumeEventInput{
		EventID: envelope.EventId, EventType: envelope.EventType, Version: int(envelope.EventVersion),
		Payload: append([]byte(nil), envelope.Payload...), ReceivedAt: c.clock().UTC(),
	})
	return err
}

func validatePayload(eventType string, version uint32, payload []byte) error {
	if version != 1 {
		return fmt.Errorf("unsupported %s version %d", eventType, version)
	}
	switch eventType {
	case "BookingConfirmed":
		var event bookingeventsv1.BookingConfirmedV1
		if err := proto.Unmarshal(payload, &event); err != nil {
			return fmt.Errorf("decode BookingConfirmedV1: %w", err)
		}
		if event.BookingId == "" || event.UserId == "" || event.HotelId == "" || event.RoomTypeId == "" ||
			event.CheckIn == "" || event.CheckOut == "" || event.TotalMinor <= 0 || event.Currency == "" {
			return errors.New("BookingConfirmedV1 has missing or invalid required fields")
		}
	case "ReservationExpired":
		var event availabilityeventsv1.ReservationExpiredV1
		if err := proto.Unmarshal(payload, &event); err != nil {
			return fmt.Errorf("decode ReservationExpiredV1: %w", err)
		}
		if event.ReservationId == "" || event.BookingId == "" || event.HotelId == "" || event.RoomTypeId == "" ||
			event.CheckIn == "" || event.CheckOut == "" || event.Quantity <= 0 {
			return errors.New("ReservationExpiredV1 has missing or invalid required fields")
		}
	default:
		return fmt.Errorf("unsupported event type %q", eventType)
	}
	return nil
}
