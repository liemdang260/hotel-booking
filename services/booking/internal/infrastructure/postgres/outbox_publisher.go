package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/internal/outbox"
)

type OutboxPublisherStore struct{ db *sql.DB }

func NewOutboxPublisherStore(db *sql.DB)*OutboxPublisherStore{return &OutboxPublisherStore{db:db}}

func(s *OutboxPublisherStore)Claim(ctx context.Context,request outbox.ClaimRequest)([]outbox.Event,error){
	tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return nil,fmt.Errorf("begin outbox claim: %w",err)}
	defer func(){_ = tx.Rollback()}()
	rows,err:=tx.QueryContext(ctx,`WITH ready AS (
 SELECT id FROM booking_outbox_events
 WHERE ((status IN ('PENDING','FAILED') AND (next_retry_at IS NULL OR next_retry_at <= $1))
        OR (status='PUBLISHING' AND locked_until <= $1))
 ORDER BY created_at,id LIMIT $2 FOR UPDATE SKIP LOCKED
)
UPDATE booking_outbox_events AS event
SET status='PUBLISHING',claim_token=$3::uuid,claimed_at=$1,locked_until=$4,last_error=NULL
FROM ready WHERE event.id=ready.id
RETURNING event.id::text,event.aggregate_type,event.aggregate_id::text,event.event_type,event.event_version,
          event.payload,event.created_at,$3::text`,
		request.Now,request.Limit,request.ClaimToken,request.LeaseUntil)
	if err!=nil{return nil,fmt.Errorf("claim booking outbox: %w",err)}
	defer rows.Close()
	var events []outbox.Event
	for rows.Next(){
		var event outbox.Event
		if err:=rows.Scan(&event.ID,&event.AggregateType,&event.AggregateID,&event.EventType,&event.EventVersion,&event.Payload,&event.OccurredAt,&event.ClaimToken);err!=nil{return nil,fmt.Errorf("scan booking outbox: %w",err)}
		events=append(events,event)
	}
	if err:=rows.Err();err!=nil{return nil,fmt.Errorf("iterate booking outbox: %w",err)}
	if err:=rows.Close();err!=nil{return nil,fmt.Errorf("close booking outbox rows: %w",err)}
	if err:=tx.Commit();err!=nil{return nil,fmt.Errorf("commit booking outbox claim: %w",err)}
	return events,nil
}

func(s *OutboxPublisherStore)MarkPublished(ctx context.Context,id,token string,publishedAt time.Time)error{
	result,err:=s.db.ExecContext(ctx,`UPDATE booking_outbox_events
 SET status='PUBLISHED',published_at=$3,claim_token=NULL,claimed_at=NULL,locked_until=NULL,last_error=NULL
 WHERE id=$1::uuid AND claim_token=$2::uuid AND status='PUBLISHING'`,id,token,publishedAt)
	return requireOne(result,err,"mark booking outbox published")
}
func(s *OutboxPublisherStore)MarkRetry(ctx context.Context,id,token string,nextAttempt time.Time,message string)error{
	result,err:=s.db.ExecContext(ctx,`UPDATE booking_outbox_events
 SET status='PENDING',retry_count=retry_count+1,next_retry_at=$3,claim_token=NULL,claimed_at=NULL,locked_until=NULL,last_error=left($4,1024)
 WHERE id=$1::uuid AND claim_token=$2::uuid AND status='PUBLISHING'`,id,token,nextAttempt,message)
	return requireOne(result,err,"mark booking outbox retry")
}
func requireOne(result sql.Result,err error,operation string)error{
	if err!=nil{return fmt.Errorf("%s: %w",operation,err)}
	rows,err:=result.RowsAffected();if err!=nil{return fmt.Errorf("%s rows: %w",operation,err)}
	if rows!=1{return fmt.Errorf("%s: lease lost",operation)}
	return nil
}
var _ outbox.Repository=(*OutboxPublisherStore)(nil)
