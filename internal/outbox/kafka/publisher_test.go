package kafka

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/internal/outbox"
)
type captureWriter struct{ message Message }
func(w *captureWriter)WriteMessage(_ context.Context,m Message)error{w.message=m;return nil}
func TestPublisherUsesAggregateKeyAndVersionedEnvelope(t *testing.T){
	writer:=&captureWriter{}
	p,err:=NewPublisher(writer,map[string]string{"booking":"booking.events.v1"});if err!=nil{t.Fatal(err)}
	event:=outbox.Event{ID:"event-1",AggregateType:"booking",AggregateID:"booking-1",AggregateVersion:7,EventType:"BookingConfirmed",EventVersion:1,Payload:[]byte(`{"currency":"USD"}`),OccurredAt:time.Unix(100,0).UTC()}
	if err:=p.Publish(context.Background(),event);err!=nil{t.Fatal(err)}
	if writer.message.Topic!="booking.events.v1"||string(writer.message.Key)!="booking-1"{t.Fatalf("unexpected routing: %#v",writer.message)}
	var envelope map[string]any
	if err:=json.Unmarshal(writer.message.Value,&envelope);err!=nil{t.Fatal(err)}
	if envelope["event_id"]!="event-1"||envelope["event_type"]!="BookingConfirmed"{t.Fatalf("unexpected envelope: %#v",envelope)}
}
