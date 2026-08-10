package domain

import (
	"context"
	"errors"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConcurrentWrite = errors.New("concurrent write")
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
)

type BookingRepository interface {
	Create(context.Context, *Booking) error
	Find(context.Context, string) (*Booking, error)
	Lock(context.Context, string) (*Booking, error)
	Save(context.Context, *Booking) error
}
type PriceSnapshotRepository interface {
	Create(context.Context, *PriceSnapshot) error
	FindByBookingID(context.Context, string) (*PriceSnapshot, error)
}
type SagaRepository interface {
	Create(context.Context, *BookingSaga) error
	LockByBookingID(context.Context, string) (*BookingSaga, error)
	Save(context.Context, *BookingSaga) error
}
type IdempotencyRepository interface {
	Claim(context.Context, *IdempotencyRecord) error
	FindByKey(context.Context, string) (*IdempotencyRecord, error)
	Save(context.Context, *IdempotencyRecord) error
}
type OutboxRepository interface {
	Add(context.Context, *OutboxEvent) error
}
type Repositories struct {
	Bookings BookingRepository
	PriceSnapshots PriceSnapshotRepository
	Sagas SagaRepository
	Idempotency IdempotencyRepository
	Outbox OutboxRepository
}
type TransactionManager interface {
	WithinTransaction(context.Context, func(context.Context, Repositories) error) error
}
