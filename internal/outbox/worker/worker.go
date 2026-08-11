package worker

import (
	"context"
	"errors"
	"time"

	"github.com/liemdang260/hotel-booking/internal/outbox"
)

type BatchPublisher interface { ExecuteBatch(context.Context, int) (outbox.Result, error) }

type Publisher struct {
	usecase BatchPublisher
	interval time.Duration
	batchSize int
}

func NewPublisher(usecase BatchPublisher,interval time.Duration,batchSize int)(*Publisher,error){
	if usecase==nil||interval<=0||batchSize<=0{return nil,errors.New("outbox worker: usecase, interval, and batch size are required")}
	return &Publisher{usecase:usecase,interval:interval,batchSize:batchSize},nil
}

func(w *Publisher)RunOnce(ctx context.Context)(outbox.Result,error){
	return w.usecase.ExecuteBatch(ctx,w.batchSize)
}

func(w *Publisher)Run(ctx context.Context)error{
	ticker:=time.NewTicker(w.interval);defer ticker.Stop()
	for{
		if _,err:=w.RunOnce(ctx);err!=nil{return err}
		select{
		case <-ctx.Done():return ctx.Err()
		case <-ticker.C:
		}
	}
}
