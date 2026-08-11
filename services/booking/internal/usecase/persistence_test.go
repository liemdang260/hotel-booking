package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

type txManager struct {
	repos      domain.Repositories
	calls      int
	rolledBack bool
}

func (m *txManager) WithinTransaction(ctx context.Context, fn func(context.Context, domain.Repositories) error) error {
	m.calls++
	err := fn(ctx, m.repos)
	m.rolledBack = err != nil
	return err
}

type bookingRepository struct {
	create int
	save   int
	err    error
}

func (r *bookingRepository) Create(context.Context, *domain.Booking) error { r.create++; return r.err }
func (r *bookingRepository) Find(context.Context, string) (*domain.Booking, error) {
	return nil, domain.ErrNotFound
}
func (r *bookingRepository) Lock(context.Context, string) (*domain.Booking, error) {
	return nil, domain.ErrNotFound
}
func (r *bookingRepository) Save(context.Context, *domain.Booking) error { r.save++; return r.err }

type outboxRepository struct {
	adds int
	err  error
}

func (r *outboxRepository) Add(context.Context, *domain.OutboxEvent) error { r.adds++; return r.err }

type priceRepository struct{}

func (priceRepository) Create(context.Context, *domain.PriceSnapshot) error { return nil }
func (priceRepository) CreateCancellationPolicy(context.Context, *domain.CancellationPolicySnapshot) error { return nil }
func (priceRepository) FindByBookingID(context.Context, string) (*domain.PriceSnapshot, error) {
	return nil, domain.ErrNotFound
}

type sagaRepository struct{ saves int }

func (*sagaRepository) Create(context.Context, *domain.BookingSaga) error { return nil }
func (*sagaRepository) LockByBookingID(context.Context, string) (*domain.BookingSaga, error) {
	return nil, domain.ErrNotFound
}
func (s *sagaRepository) Save(context.Context, *domain.BookingSaga) error { s.saves++; return nil }

type idemRepository struct{}

func (idemRepository) Claim(context.Context, *domain.IdempotencyRecord) error { return nil }
func (idemRepository) FindByKey(context.Context, string) (*domain.IdempotencyRecord, error) {
	return nil, domain.ErrNotFound
}
func (idemRepository) Save(context.Context, *domain.IdempotencyRecord) error { return nil }

func TestSaveStateAndOutboxShareTransaction(t *testing.T) {
	bRepo := &bookingRepository{}
	oRepo := &outboxRepository{}
	sRepo := &sagaRepository{}
	tx := &txManager{repos: domain.Repositories{
		Bookings: bRepo, PriceSnapshots: priceRepository{}, Sagas: sRepo,
		Idempotency: idemRepository{}, Outbox: oRepo,
	}}
	err := NewPersistence(tx).SaveStateWithEvent(context.Background(), &domain.Booking{}, &domain.BookingSaga{}, &domain.OutboxEvent{})
	if err != nil {
		t.Fatal(err)
	}
	if tx.calls != 1 || bRepo.save != 1 || sRepo.saves != 1 || oRepo.adds != 1 {
		t.Fatalf("writes did not share one transaction")
	}
}

func TestOutboxFailureRollsBackLocalState(t *testing.T) {
	bRepo := &bookingRepository{}
	oRepo := &outboxRepository{err: errors.New("outbox unavailable")}
	tx := &txManager{repos: domain.Repositories{Bookings: bRepo, Sagas: &sagaRepository{}, Outbox: oRepo}}
	err := NewPersistence(tx).SaveStateWithEvent(context.Background(), &domain.Booking{}, &domain.BookingSaga{}, &domain.OutboxEvent{})
	if err == nil || !tx.rolledBack {
		t.Fatalf("expected transaction rollback")
	}
}

func TestAcceptedQuoteCannotBeExpired(t *testing.T) {
	now := time.Now()
	p := domain.PriceSnapshot{
		BookingID: "b", QuoteID: "q", Currency: "USD", PricingVersion: "v1",
		SubtotalMinor: 100, TotalMinor: 100, QuotedAt: now,
		QuoteExpiresAt: now.Add(time.Minute), AcceptedAt: now.Add(2 * time.Minute),
	}
	if err := p.Validate(); !errors.Is(err, domain.ErrInvalidPriceSnapshot) {
		t.Fatalf("expected invalid snapshot, got %v", err)
	}
}
