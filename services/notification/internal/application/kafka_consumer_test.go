package application

import (
	"context"
	"testing"
	"time"

	availabilityeventsv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/events/availability/v1"
	eventsv1 "github.com/liemdang260/hotel-booking/gen/go/hotelbooking/events/common/v1"
	"github.com/liemdang260/hotel-booking/services/notification/internal/usecase"
	"google.golang.org/protobuf/proto"
)

type captureUsecase struct {
	input usecase.ConsumeEventInput
	calls int
}

func (c *captureUsecase) Execute(_ context.Context, input usecase.ConsumeEventInput) (usecase.ConsumeEventResult, error) {
	c.input = input
	c.calls++
	return usecase.ConsumeEventResult{}, nil
}

func TestKafkaConsumerOnlyDecodesValidContractAndInvokesUsecase(t *testing.T) {
	target := &captureUsecase{}
	consumer, err := NewKafkaConsumer(target, func() time.Time { return time.Unix(100, 0) })
	if err != nil {
		t.Fatal(err)
	}
	payload, err := proto.Marshal(&availabilityeventsv1.ReservationExpiredV1{
		ReservationId: "reservation-1", BookingId: "booking-1", HotelId: "hotel-1",
		RoomTypeId: "room-type-1", CheckIn: "2026-08-12", CheckOut: "2026-08-13", Quantity: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := proto.Marshal(&eventsv1.EventEnvelope{
		EventId: "10000000-0000-4000-8000-000000000001", EventType: "ReservationExpired",
		EventVersion: 1, Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.Handle(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	if target.calls != 1 || target.input.EventType != "ReservationExpired" || target.input.Version != 1 {
		t.Fatalf("unexpected invocation: calls=%d input=%#v", target.calls, target.input)
	}
}

func TestKafkaConsumerRejectsMalformedContractBeforeUsecase(t *testing.T) {
	target := &captureUsecase{}
	consumer, err := NewKafkaConsumer(target, func() time.Time { return time.Unix(100, 0) })
	if err != nil {
		t.Fatal(err)
	}
	payload, err := proto.Marshal(&availabilityeventsv1.ReservationExpiredV1{ReservationId: "reservation-1"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := proto.Marshal(&eventsv1.EventEnvelope{
		EventId: "10000000-0000-4000-8000-000000000002", EventType: "ReservationExpired",
		EventVersion: 1, Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.Handle(context.Background(), value); err == nil {
		t.Fatal("expected malformed contract error")
	}
	if target.calls != 0 {
		t.Fatalf("malformed event invoked usecase %d times", target.calls)
	}
}
