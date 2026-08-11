package domain

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrInvalidIntegrationEvent=errors.New("invalid integration event")

type IntegrationEventEnvelope struct {
	EventID string `json:"event_id"`
	EventType string `json:"event_type"`
	EventVersion int `json:"event_version"`
	AggregateType string `json:"aggregate_type"`
	AggregateID string `json:"aggregate_id"`
	AggregateVersion int64 `json:"aggregate_version"`
	OccurredAt time.Time `json:"occurred_at"`
	CorrelationID string `json:"correlation_id"`
	CausationID string `json:"causation_id"`
	Payload json.RawMessage `json:"payload"`
}
func(e IntegrationEventEnvelope)Validate()error{
	if e.EventID==""||e.EventType==""||e.EventVersion<1||e.AggregateType==""||e.AggregateID==""||e.AggregateVersion<1||e.OccurredAt.IsZero()||e.CorrelationID==""||e.CausationID==""||!json.Valid(e.Payload){return ErrInvalidIntegrationEvent}
	return nil
}
func NewBookingCancelledEvent(eventID string,b Booking,c BookingCancellation,now time.Time)(OutboxEvent,error){
	payload,_:=json.Marshal(map[string]any{"booking_id":b.ID,"user_id":b.UserID,"refund_amount_minor":c.RefundAmountMinor,"currency":c.Currency,"cancellation_id":c.ID})
	env:=IntegrationEventEnvelope{EventID:eventID,EventType:"BookingCancelled",EventVersion:1,AggregateType:"booking",AggregateID:b.ID,AggregateVersion:b.Version+1,OccurredAt:now.UTC(),CorrelationID:c.ID,CausationID:c.IdempotencyKey,Payload:payload}
	if err:=env.Validate();err!=nil{return OutboxEvent{},err}
	body,err:=json.Marshal(env);if err!=nil{return OutboxEvent{},err}
	return OutboxEvent{ID:eventID,AggregateType:"booking",AggregateID:b.ID,EventType:"BookingCancelled",EventVersion:1,Payload:body,Status:OutboxPending,CreatedAt:now.UTC()},nil
}
func NewRefundCompletedEvent(eventID string,b Booking,c BookingCancellation,now time.Time)(OutboxEvent,error){
	payload,_:=json.Marshal(map[string]any{"booking_id":b.ID,"refund_id":c.RefundID,"refund_amount_minor":c.RefundAmountMinor,"currency":c.Currency})
	env:=IntegrationEventEnvelope{EventID:eventID,EventType:"BookingRefundCompleted",EventVersion:1,AggregateType:"booking",AggregateID:b.ID,AggregateVersion:b.Version,OccurredAt:now.UTC(),CorrelationID:c.ID,CausationID:c.RefundID,Payload:payload}
	if err:=env.Validate();err!=nil{return OutboxEvent{},err};body,err:=json.Marshal(env);if err!=nil{return OutboxEvent{},err}
	return OutboxEvent{ID:eventID,AggregateType:"booking",AggregateID:b.ID,EventType:"BookingRefundCompleted",EventVersion:1,Payload:body,Status:OutboxPending,CreatedAt:now.UTC()},nil
}
