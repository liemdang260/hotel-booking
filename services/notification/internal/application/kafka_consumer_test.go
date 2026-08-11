package application

import (
	"context"
	"testing"
	"time"

	eventsv1 "github.com/liemdang260/hotel-booking/gen/go/events/common/v1"
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

func TestKafkaConsumerOnlyDecodesAndInvokesUsecase(t *testing.T) {
	target := &captureUsecase{}
	consumer, err := NewKafkaConsumer(target, func() time.Time { return time.Unix(100, 0) })
	if err != nil {
		t.Fatal(err)
	}
	value, err := proto.Marshal(&eventsv1.EventEnvelope{
		EventId: "10000000-0000-4000-8000-000000000001", EventType: "ReservationExpired",
		EventVersion: 1, Payload: []byte{1, 2, 3},
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
