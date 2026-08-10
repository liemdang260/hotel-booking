package worker

import (
	"context"
	"time"
)
type RecoveryExecutor interface{RecoverDue(context.Context,int)(int,error)}
type RecoveryWorker struct{
	usecase RecoveryExecutor
	interval time.Duration
	batchSize int
}
func NewRecoveryWorker(u RecoveryExecutor,interval time.Duration,batchSize int)*RecoveryWorker{
	return &RecoveryWorker{usecase:u,interval:interval,batchSize:batchSize}
}
func(w *RecoveryWorker)Run(ctx context.Context)error{
	ticker:=time.NewTicker(w.interval);defer ticker.Stop()
	for{
		select{
		case <-ctx.Done():return ctx.Err()
		case <-ticker.C:
			if _,err:=w.usecase.RecoverDue(ctx,w.batchSize);err!=nil{return err}
		}
	}
}
func(w *RecoveryWorker)RunOnce(ctx context.Context)(int,error){
	return w.usecase.RecoverDue(ctx,w.batchSize)
}
