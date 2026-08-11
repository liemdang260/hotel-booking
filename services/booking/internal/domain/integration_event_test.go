package domain

import (
	"encoding/json"
	"testing"
	"time"
)
func TestCancellationLifecycleEventsUseDistinctVersionedEnvelopes(t *testing.T){
	now:=time.Date(2026,8,11,9,0,0,0,time.UTC)
	b:=Booking{ID:"b1",UserID:"u1",Version:7}
	c:=BookingCancellation{ID:"c1",IdempotencyKey:"cancel-key",RefundID:"r1",RefundAmountMinor:9000,Currency:"USD"}
	cancelled,err:=NewBookingCancelledEvent("e1",b,c,now);if err!=nil{t.Fatal(err)}
	refunded,err:=NewRefundCompletedEvent("e2",b,c,now.Add(time.Minute));if err!=nil{t.Fatal(err)}
	if cancelled.EventType!="BookingCancelled"||refunded.EventType!="BookingRefundCompleted"{t.Fatalf("events=%s,%s",cancelled.EventType,refunded.EventType)}
	var a,z IntegrationEventEnvelope
	if err=json.Unmarshal(cancelled.Payload,&a);err!=nil{t.Fatal(err)};if err=json.Unmarshal(refunded.Payload,&z);err!=nil{t.Fatal(err)}
	if a.EventVersion!=1||a.AggregateVersion!=8||a.CorrelationID!="c1"||a.CausationID!="cancel-key"{t.Fatalf("cancel envelope=%+v",a)}
	if z.CausationID!="r1"||z.OccurredAt.Equal(a.OccurredAt){t.Fatalf("refund envelope=%+v",z)}
}
