package usecase

import (
	"context"
	"fmt"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

// Persistence coordinates only local durable writes. Network calls must happen
// outside this boundary and their result is then recorded in a short transaction.
type Persistence struct { transactions domain.TransactionManager }

func NewPersistence(transactions domain.TransactionManager) *Persistence { return &Persistence{transactions:transactions} }

func (p *Persistence) CreateAttempt(
	ctx context.Context,
	booking *domain.Booking,
	snapshot *domain.PriceSnapshot,
	saga *domain.BookingSaga,
	idempotency *domain.IdempotencyRecord,
	event *domain.OutboxEvent,
	cancellation ...*domain.CancellationPolicySnapshot,
) error {
	if err:=booking.Validate();err!=nil{return err}
	if err:=snapshot.Validate();err!=nil{return err}
	var policy *domain.CancellationPolicySnapshot
	if len(cancellation)>0 {
		policy=cancellation[0]
		if policy==nil{return domain.ErrInvalidPriceSnapshot}
		if err:=policy.Validate();err!=nil{return err}
		if policy.BookingID!=booking.ID{return fmt.Errorf("cancellation snapshot booking mismatch")}
	}
	return p.transactions.WithinTransaction(ctx,func(txctx context.Context,r domain.Repositories)error{
		if err:=r.Bookings.Create(txctx,booking);err!=nil{return fmt.Errorf("create booking: %w",err)}
		if err:=r.PriceSnapshots.Create(txctx,snapshot);err!=nil{return fmt.Errorf("snapshot accepted quote: %w",err)}
		if policy!=nil { if err:=r.PriceSnapshots.CreateCancellationPolicy(txctx,policy);err!=nil{return fmt.Errorf("snapshot accepted cancellation policy: %w",err)} }
		if err:=r.Sagas.Create(txctx,saga);err!=nil{return fmt.Errorf("create saga: %w",err)}
		if err:=r.Idempotency.Claim(txctx,idempotency);err!=nil{return fmt.Errorf("claim idempotency key: %w",err)}
		if err:=r.Outbox.Add(txctx,event);err!=nil{return fmt.Errorf("append outbox event: %w",err)}
		return nil
	})
}

func(p *Persistence)SaveStateWithEvent(ctx context.Context,booking *domain.Booking,saga *domain.BookingSaga,event *domain.OutboxEvent)error{
	return p.transactions.WithinTransaction(ctx,func(txctx context.Context,r domain.Repositories)error{
		if err:=r.Bookings.Save(txctx,booking);err!=nil{return fmt.Errorf("save booking: %w",err)}
		if err:=r.Sagas.Save(txctx,saga);err!=nil{return fmt.Errorf("save saga: %w",err)}
		if err:=r.Outbox.Add(txctx,event);err!=nil{return fmt.Errorf("append outbox event: %w",err)}
		return nil
	})
}
