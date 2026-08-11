package usecase

import (
	"context"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

type CreatePaymentExecutor interface {
	Execute(context.Context, CreatePaymentInput) (domain.Payment, error)
}

// CreatePaymentWithReconciliation decorates the authoritative CreatePayment
// usecase. It never issues a second charge; it only ensures an UNKNOWN result has
// one durable reconciliation job.
type CreatePaymentWithReconciliation struct {
	create CreatePaymentExecutor
	reconciliations repository.ReconciliationRepository
	clock Clock
	firstDelay time.Duration
	maxAttempts int
}

func NewCreatePaymentWithReconciliation(
	create CreatePaymentExecutor,
	reconciliations repository.ReconciliationRepository,
	clock Clock,
	firstDelay time.Duration,
	maxAttempts int,
) *CreatePaymentWithReconciliation {
	return &CreatePaymentWithReconciliation{
		create:create, reconciliations:reconciliations, clock:clock,
		firstDelay:firstDelay, maxAttempts:maxAttempts,
	}
}

func (u *CreatePaymentWithReconciliation) Execute(ctx context.Context, in CreatePaymentInput) (domain.Payment,error) {
	payment,err:=u.create.Execute(ctx,in)
	if err!=nil{return domain.Payment{},err}
	if payment.Status!=domain.StatusUnknown{return payment,nil}
	now:=u.clock.Now().UTC()
	if err:=u.reconciliations.EnsurePending(ctx,payment.ID,now.Add(u.firstDelay),u.maxAttempts,now);err!=nil{
		return domain.Payment{},err
	}
	return payment,nil
}
