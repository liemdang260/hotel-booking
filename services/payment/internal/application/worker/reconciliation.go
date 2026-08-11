package worker

import (
	"context"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
	"github.com/liemdang260/hotel-booking/services/payment/internal/usecase"
)

type ReconcileUsecase interface {
	Execute(context.Context,repository.ReconciliationJob)(usecase.ReconciliationResult,error)
}

type ReconciliationWorker struct{
	repository repository.ReconciliationRepository
	reconcile ReconcileUsecase
	clock usecase.Clock
	batchSize int
	leaseDuration time.Duration
}

func NewReconciliationWorker(r repository.ReconciliationRepository,u ReconcileUsecase,c usecase.Clock,batch int,lease time.Duration)*ReconciliationWorker{
	return &ReconciliationWorker{repository:r,reconcile:u,clock:c,batchSize:batch,leaseDuration:lease}
}

// RunOnce only claims bounded work and delegates all state decisions to the usecase.
// ClaimDue commits before provider calls, so no database lock crosses the network.
func(w *ReconciliationWorker)RunOnce(ctx context.Context)(int,error){
	now:=w.clock.Now().UTC()
	jobs,err:=w.repository.ClaimDue(ctx,now,now.Add(w.leaseDuration),w.batchSize)
	if err!=nil{return 0,err}
	processed:=0
	for _,job:=range jobs{
		if ctx.Err()!=nil{return processed,ctx.Err()}
		if _,err:=w.reconcile.Execute(ctx,job);err!=nil{return processed,err}
		processed++
	}
	return processed,nil
}
