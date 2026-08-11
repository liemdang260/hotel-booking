package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrConcurrentRecovery = errors.New("saga was claimed by another worker")
	ErrRetryableRecovery = errors.New("retryable recovery failure")
)

type SagaSnapshot struct {
	ID, BookingID, State, ReservationID, PaymentID string
	Version int64
	RetryCount int
	NextRetryAt time.Time
}
type SagaRecoveryStore interface {
	// ClaimDue must lease rows atomically and increment/guard Version so two
	// workers cannot own the same recovery attempt.
	ClaimDue(context.Context,time.Time,int,time.Duration)([]SagaSnapshot,error)
	MarkRecovered(context.Context,string,int64) error
	ScheduleRetry(context.Context,string,int64,time.Time,string) error
}
type SagaResumer interface {
	// Resume reconciles persisted identifiers before any mutation. Repeated
	// calls for one BookingID must use the same downstream command identities.
	Resume(context.Context,SagaSnapshot) error
}
type RecoveryUsecase struct {
	store SagaRecoveryStore
	resumer SagaResumer
	now func()time.Time
	baseBackoff,maxBackoff time.Duration
	lease time.Duration
}
func NewRecoveryUsecase(s SagaRecoveryStore,r SagaResumer)*RecoveryUsecase{
	return &RecoveryUsecase{store:s,resumer:r,now:time.Now,baseBackoff:time.Second,maxBackoff:time.Minute,lease:30*time.Second}
}
func(u *RecoveryUsecase)RecoverDue(ctx context.Context,limit int)(int,error){
	if limit<1{return 0,nil}
	sagas,err:=u.store.ClaimDue(ctx,u.now(),limit,u.lease)
	if err!=nil{return 0,fmt.Errorf("claim recovery batch: %w",err)}
	processed:=0
	for _,saga:=range sagas{
		if err=ctx.Err();err!=nil{return processed,err}
		err=u.resumer.Resume(ctx,saga)
		switch{
		case err==nil:
			err=u.store.MarkRecovered(ctx,saga.ID,saga.Version)
		case errors.Is(err,ErrConcurrentRecovery):
			continue
		default:
			next:=u.now().Add(u.backoff(saga.RetryCount))
			err=u.store.ScheduleRetry(ctx,saga.ID,saga.Version,next,errorCode(err))
		}
		if errors.Is(err,ErrConcurrentRecovery){continue}
		if err!=nil{return processed,fmt.Errorf("persist recovery result for %s: %w",saga.ID,err)}
		processed++
	}
	return processed,nil
}
func(u *RecoveryUsecase)backoff(retry int)time.Duration{
	if retry<0{retry=0};d:=u.baseBackoff
	for i:=0;i<retry&&d<u.maxBackoff;i++{d*=2;if d>u.maxBackoff{d=u.maxBackoff}}
	return d
}
func errorCode(err error)string{
	if errors.Is(err,ErrRetryableRecovery){return "RETRYABLE"}
	return "RECOVERY_FAILED"
}
