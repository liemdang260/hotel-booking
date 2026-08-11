package application

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	_, err := c.consume.Execute(ctx, usecase.ConsumeEventInput{
		EventID: envelope.EventId, EventType: envelope.EventType, Version: int(envelope.EventVersion),
		Payload: append([]byte(nil), envelope.Payload...), ReceivedAt: c.clock().UTC(),
	})
	return err
}
