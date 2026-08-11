package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/liemdang260/hotel-booking/internal/outbox"
)

type Message struct {
	Topic   string
	Key     []byte
	Value   []byte
	Headers map[string]string
}

type Writer interface { WriteMessage(context.Context, Message) error }

type Publisher struct {
	writer Writer
	topics map[string]string
}

func NewPublisher(writer Writer, topics map[string]string) (*Publisher,error) {
	if writer==nil||len(topics)==0{return nil,errors.New("outbox kafka: writer and topics are required")}
	copied:=make(map[string]string,len(topics))
	for aggregate,topic:=range topics{
		if aggregate==""||topic==""{return nil,errors.New("outbox kafka: aggregate and topic are required")}
		copied[aggregate]=topic
	}
	return &Publisher{writer:writer,topics:copied},nil
}

func(p *Publisher)Publish(ctx context.Context,event outbox.Event)error{
	topic,ok:=p.topics[event.AggregateType]
	if !ok{return fmt.Errorf("outbox kafka: no topic for aggregate type %q",event.AggregateType)}
	if event.ID==""||event.AggregateID==""||event.EventType==""||event.EventVersion<=0||!json.Valid(event.Payload){
		return errors.New("outbox kafka: invalid event")
	}
	envelope:=struct{
		EventID string `json:"event_id"`
		EventType string `json:"event_type"`
		EventVersion int `json:"event_version"`
		AggregateType string `json:"aggregate_type"`
		AggregateID string `json:"aggregate_id"`
		AggregateVersion int64 `json:"aggregate_version,omitempty"`
		OccurredAt any `json:"occurred_at"`
		CorrelationID string `json:"correlation_id,omitempty"`
		CausationID string `json:"causation_id,omitempty"`
		Payload json.RawMessage `json:"payload"`
	}{
		EventID:event.ID,EventType:event.EventType,EventVersion:event.EventVersion,
		AggregateType:event.AggregateType,AggregateID:event.AggregateID,AggregateVersion:event.AggregateVersion,
		OccurredAt:event.OccurredAt.UTC(),CorrelationID:event.CorrelationID,CausationID:event.CausationID,Payload:event.Payload,
	}
	value,err:=json.Marshal(envelope);if err!=nil{return fmt.Errorf("outbox kafka: marshal envelope: %w",err)}
	headers:=map[string]string{
		"event_id":event.ID,"event_type":event.EventType,"event_version":strconv.Itoa(event.EventVersion),
		"content-type":"application/json",
	}
	if event.CorrelationID!=""{headers["correlation_id"]=event.CorrelationID}
	if event.CausationID!=""{headers["causation_id"]=event.CausationID}
	return p.writer.WriteMessage(ctx,Message{Topic:topic,Key:[]byte(event.AggregateID),Value:value,Headers:headers})
}
