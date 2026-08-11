package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/provider"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

var ErrInvalidRetryPolicy = errors.New("invalid reconciliation retry policy")

type BackoffPolicy struct { Delays []time.Duration }

func (p BackoffPolicy) Delay(retryCount int) (time.Duration,error) {
	if retryCount<1||retryCount>len(p.Delays)||p.Delays[retryCount-1]<=0{
		return 0,ErrInvalidRetryPolicy
	}
	return p.Delays[retryCount-1],nil
}

type ReconciliationResult struct {
	Payment domain.Payment
	Resolved bool
	Exhausted bool
	NextRetryAt *time.Time
}

type ReconcilePayment struct {
	reconciliations repository.ReconciliationRepository
	provider provider.PaymentLookup
	clock Clock
	backoff BackoffPolicy
}

func NewReconcilePayment(r repository.ReconciliationRepository,p provider.PaymentLookup,c Clock,b BackoffPolicy)*ReconcilePayment{
	return &ReconcilePayment{reconciliations:r,provider:p,clock:c,backoff:b}
}

func(u *ReconcilePayment)Execute(ctx context.Context,job repository.ReconciliationJob)(ReconciliationResult,error){
	result:=u.provider.GetPayment(ctx,provider.LookupRequest{
		PaymentID:job.PaymentID,IdempotencyKey:job.IdempotencyKey,
		ProviderReference:job.ProviderReference,
	})
	now:=u.clock.Now().UTC()
	switch result.Outcome {
	case domain.AttemptSucceeded:
		payment,err:=u.reconciliations.Resolve(ctx,job.PaymentID,job.Version,domain.StatusSucceeded,result.ProviderReference,"",now)
		return ReconciliationResult{Payment:payment,Resolved:err==nil},err
	case domain.AttemptDeclined:
		payment,err:=u.reconciliations.Resolve(ctx,job.PaymentID,job.Version,domain.StatusFailed,result.ProviderReference,result.FailureCode,now)
		return ReconciliationResult{Payment:payment,Resolved:err==nil},err
	case domain.AttemptUnknown:
		nextCount:=job.RetryCount+1
		if nextCount>=job.MaxAttempts{
			err:=u.reconciliations.Exhaust(ctx,job.PaymentID,job.Version,nextCount,result.FailureCode,now)
			return ReconciliationResult{Exhausted:err==nil},err
		}
		delay,err:=u.backoff.Delay(nextCount);if err!=nil{return ReconciliationResult{},err}
		next:=now.Add(delay)
		err=u.reconciliations.Reschedule(ctx,job.PaymentID,job.Version,nextCount,next,result.FailureCode,now)
		return ReconciliationResult{NextRetryAt:&next},err
	default:
		return ReconciliationResult{},ErrInvalidRetryPolicy
	}
}
