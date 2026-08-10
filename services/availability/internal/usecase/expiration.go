package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/domain/repository"
)

var (
	ErrExpirationBatchSize = errors.New("availability: expiration batch size must be positive")
	ErrExpirationInvariant = errors.New("availability: expiration inventory invariant violated")
)

type ExpirationClock interface {
	Now() time.Time
}

type EventIDGenerator interface {
	NewEventID() domain.EventID
}

type ExpiredReservationRepository interface {
	LockExpired(ctx context.Context, expiresAtOrBefore time.Time, limit int) ([]domain.Reservation, error)
}

type ExpirationRepositories struct {
	Inventory   repository.InventoryRepository
	Reservation repository.ReservationRepository
	Expired     ExpiredReservationRepository
	Outbox      repository.OutboxRepository
}

type ExpirationTransactionWork func(context.Context, ExpirationRepositories) error

type ExpirationTransactionManager interface {
	WithinExpirationTransaction(context.Context, ExpirationTransactionWork) error
}

type ExpireReservations struct {
	transactions ExpirationTransactionManager
	events       EventIDGenerator
	clock        ExpirationClock
	maxBatchSize int
}

func NewExpireReservations(transactions ExpirationTransactionManager, events EventIDGenerator, clock ExpirationClock, maxBatchSize int) *ExpireReservations {
	return &ExpireReservations{
		transactions: transactions,
		events: events,
		clock: clock,
		maxBatchSize: maxBatchSize,
	}
}

func (u *ExpireReservations) ExecuteBatch(ctx context.Context, requestedLimit int) (int, error) {
	if u.transactions == nil || u.events == nil || u.clock == nil || u.maxBatchSize <= 0 || requestedLimit <= 0 {
		return 0, ErrExpirationBatchSize
	}
	limit := requestedLimit
	if limit > u.maxBatchSize {
		limit = u.maxBatchSize
	}
	now := u.clock.Now().UTC()
	expiredCount := 0
	err := u.transactions.WithinExpirationTransaction(ctx, func(ctx context.Context, repos ExpirationRepositories) error {
		reservations, err := repos.Expired.LockExpired(ctx, now, limit)
		if err != nil {
			return fmt.Errorf("claim expired reservations: %w", err)
		}
		for i := range reservations {
			reservation := &reservations[i]
			if reservation.Status != domain.ReservationHeld || reservation.ExpiresAt == nil || reservation.ExpiresAt.After(now) {
				return ErrExpirationInvariant
			}
			if err := releaseExpiredInventory(ctx, repos, reservation); err != nil {
				return err
			}
			reservation.Status = domain.ReservationExpired
			reservation.UpdatedAt = now
			if err := repos.Reservation.SaveReservation(ctx, *reservation); err != nil {
				return fmt.Errorf("save expired reservation %s: %w", reservation.ID, err)
			}
			event, err := u.expiredEvent(*reservation, now)
			if err != nil {
				return err
			}
			if err := repos.Outbox.Add(ctx, event); err != nil {
				return fmt.Errorf("persist ReservationExpired for %s: %w", reservation.ID, err)
			}
			expiredCount++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return expiredCount, nil
}

func releaseExpiredInventory(ctx context.Context, repos ExpirationRepositories, reservation *domain.Reservation) error {
	inventory, err := repos.Inventory.LockRange(ctx, reservation.HotelID, reservation.RoomTypeID, reservation.CheckIn, reservation.CheckOut)
	if err != nil {
		return fmt.Errorf("lock inventory for reservation %s: %w", reservation.ID, err)
	}
	expected := int(reservation.CheckOut.Sub(reservation.CheckIn) / (24 * time.Hour))
	if len(inventory) != expected || expected <= 0 {
		return ErrExpirationInvariant
	}
	expectedDate := reservation.CheckIn
	for i := range inventory {
		item := &inventory[i]
		if !item.Date.Equal(expectedDate) || item.HeldQuantity < reservation.Quantity {
			return ErrExpirationInvariant
		}
		item.HeldQuantity -= reservation.Quantity
		if err := repos.Inventory.SaveInventory(ctx, item); err != nil {
			return fmt.Errorf("release expired inventory %s: %w", item.Date.Format(time.DateOnly), err)
		}
		expectedDate = expectedDate.AddDate(0, 0, 1)
	}
	return nil
}

func (u *ExpireReservations) expiredEvent(reservation domain.Reservation, now time.Time) (domain.OutboxEvent, error) {
	eventID := u.events.NewEventID()
	if eventID == "" {
		return domain.OutboxEvent{}, errors.New("availability: generated event id is empty")
	}
	payload, err := json.Marshal(struct {
		ReservationID domain.ReservationID `json:"reservation_id"`
		BookingID     domain.BookingID     `json:"booking_id"`
		ExpiredAt     time.Time            `json:"expired_at"`
	}{
		ReservationID: reservation.ID,
		BookingID: reservation.BookingID,
		ExpiredAt: now,
	})
	if err != nil {
		return domain.OutboxEvent{}, fmt.Errorf("marshal ReservationExpired: %w", err)
	}
	return domain.OutboxEvent{
		ID: eventID,
		AggregateType: "reservation",
		AggregateID: string(reservation.ID),
		AggregateVersion: 1,
		EventType: "ReservationExpired",
		Payload: payload,
		AvailableAt: now,
		CreatedAt: now,
	}, nil
}
