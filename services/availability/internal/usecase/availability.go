package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain"
	"github.com/liemdang260/hotel-booking/services/availability/internal/domain/repository"
)

var (
	ErrInvalidRequest      = errors.New("availability: invalid request")
	ErrInventoryIncomplete = errors.New("availability: inventory range is incomplete")
	ErrSoldOut             = errors.New("availability: requested inventory is unavailable")
	ErrIdempotencyConflict = errors.New("availability: booking already has a different reservation")
	ErrInvalidTransition   = errors.New("availability: invalid reservation transition")
)

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewReservationID() domain.ReservationID
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type CheckAvailabilityInput struct {
	HotelID    domain.HotelID
	RoomTypeID domain.RoomTypeID
	CheckIn    time.Time
	CheckOut   time.Time
	Quantity   int
}

type AvailabilityResult struct {
	AvailableQuantity int
	Available         bool
}

type ReserveInventoryInput struct {
	BookingID  domain.BookingID
	HotelID    domain.HotelID
	RoomTypeID domain.RoomTypeID
	CheckIn    time.Time
	CheckOut   time.Time
	Quantity   int
	HoldTTL    time.Duration
}

type ReservationResult struct {
	ReservationID domain.ReservationID
	Status        domain.ReservationStatus
	ExpiresAt     *time.Time
}

type Service struct {
	transactions TransactionBoundary
	ids          IDGenerator
	clock        Clock
}

func NewService(transactions TransactionBoundary, ids IDGenerator, clock Clock) *Service {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Service{transactions: transactions, ids: ids, clock: clock}
}

func (s *Service) CheckAvailability(ctx context.Context, input CheckAvailabilityInput) (AvailabilityResult, error) {
	if err := validateRange(input.HotelID, input.RoomTypeID, input.CheckIn, input.CheckOut, input.Quantity); err != nil {
		return AvailabilityResult{}, err
	}

	result := AvailabilityResult{}
	err := s.transactions.Execute(ctx, func(ctx context.Context, repos repository.Repositories) error {
		inventory, err := repos.Inventory.LockRange(ctx, input.HotelID, input.RoomTypeID, input.CheckIn, input.CheckOut)
		if err != nil {
			return fmt.Errorf("check inventory: %w", err)
		}
		minimum, err := minimumAvailability(inventory, input.CheckIn, input.CheckOut)
		if err != nil {
			return err
		}
		result.AvailableQuantity = minimum
		result.Available = minimum >= input.Quantity
		return nil
	})
	return result, err
}

func (s *Service) ReserveInventory(ctx context.Context, input ReserveInventoryInput) (ReservationResult, error) {
	if err := validateRange(input.HotelID, input.RoomTypeID, input.CheckIn, input.CheckOut, input.Quantity); err != nil {
		return ReservationResult{}, err
	}
	if input.BookingID == "" || input.HoldTTL <= 0 || s.ids == nil {
		return ReservationResult{}, ErrInvalidRequest
	}

	var result ReservationResult
	err := s.transactions.Execute(ctx, func(ctx context.Context, repos repository.Repositories) error {
		existing, err := repos.Reservation.FindByBookingID(ctx, input.BookingID)
		switch {
		case err == nil:
			if !sameReservation(existing, input) {
				return ErrIdempotencyConflict
			}
			if existing.Status != domain.ReservationHeld && existing.Status != domain.ReservationBooked {
				return ErrInvalidTransition
			}
			result = reservationResult(existing)
			return nil
		case !errors.Is(err, repository.ErrNotFound):
			return fmt.Errorf("find reservation by booking: %w", err)
		}

		inventory, err := repos.Inventory.LockRange(ctx, input.HotelID, input.RoomTypeID, input.CheckIn, input.CheckOut)
		if err != nil {
			return fmt.Errorf("lock inventory: %w", err)
		}
		minimum, err := minimumAvailability(inventory, input.CheckIn, input.CheckOut)
		if err != nil {
			return err
		}
		if minimum < input.Quantity {
			return ErrSoldOut
		}

		now := s.clock.Now().UTC()
		expiresAt := now.Add(input.HoldTTL)
		reservation := domain.Reservation{
			ID: s.ids.NewReservationID(), BookingID: input.BookingID,
			HotelID: input.HotelID, RoomTypeID: input.RoomTypeID,
			CheckIn: input.CheckIn, CheckOut: input.CheckOut, Quantity: input.Quantity,
			Status: domain.ReservationHeld, ExpiresAt: &expiresAt,
			CreatedAt: now, UpdatedAt: now,
		}
		if reservation.ID == "" {
			return errors.New("availability: generated reservation id is empty")
		}

		items := make([]domain.ReservationInventory, 0, len(inventory))
		for i := range inventory {
			inventory[i].HeldQuantity += input.Quantity
			if err := repos.Inventory.SaveInventory(ctx, &inventory[i]); err != nil {
				return fmt.Errorf("reserve inventory date %s: %w", inventory[i].Date.Format(time.DateOnly), err)
			}
			items = append(items, domain.ReservationInventory{
				ReservationID: reservation.ID, Date: inventory[i].Date, Quantity: input.Quantity,
			})
		}
		if err := repos.Reservation.Create(ctx, reservation); err != nil {
			return fmt.Errorf("create reservation: %w", err)
		}
		if err := repos.Reservation.AddInventory(ctx, items); err != nil {
			return fmt.Errorf("record reservation inventory: %w", err)
		}
		result = reservationResult(&reservation)
		return nil
	})
	return result, err
}

func (s *Service) ConfirmReservation(ctx context.Context, reservationID domain.ReservationID) (ReservationResult, error) {
	return s.transition(ctx, reservationID, domain.ReservationBooked)
}

func (s *Service) ReleaseReservation(ctx context.Context, reservationID domain.ReservationID) (ReservationResult, error) {
	return s.transition(ctx, reservationID, domain.ReservationReleased)
}

// CancelBookedReservation is deliberately separate from ReleaseReservation:
// BOOKED cancellation returns booked inventory, while release only removes a HELD hold.
func (s *Service) CancelBookedReservation(ctx context.Context, reservationID domain.ReservationID) (ReservationResult, error) {
	if reservationID == "" {
		return ReservationResult{}, ErrInvalidRequest
	}

	var result ReservationResult
	err := s.transactions.Execute(ctx, func(ctx context.Context, repos repository.Repositories) error {
		reservation, err := repos.Reservation.LockByID(ctx, reservationID)
		if err != nil {
			return fmt.Errorf("lock booked reservation: %w", err)
		}
		if reservation.Status == domain.ReservationCancelled {
			result = reservationResult(reservation)
			return nil
		}
		if reservation.Status != domain.ReservationBooked {
			return ErrInvalidTransition
		}

		inventory, err := repos.Inventory.LockRange(
			ctx,
			reservation.HotelID,
			reservation.RoomTypeID,
			reservation.CheckIn,
			reservation.CheckOut,
		)
		if err != nil {
			return fmt.Errorf("lock booked reservation inventory: %w", err)
		}
		if _, err := minimumAvailability(inventory, reservation.CheckIn, reservation.CheckOut); err != nil {
			return err
		}
		for i := range inventory {
			if inventory[i].BookedQuantity < reservation.Quantity {
				return ErrInvalidTransition
			}
			inventory[i].BookedQuantity -= reservation.Quantity
			if err := repos.Inventory.SaveInventory(ctx, &inventory[i]); err != nil {
				return fmt.Errorf(
					"save booked cancellation inventory date %s: %w",
					inventory[i].Date.Format(time.DateOnly),
					err,
				)
			}
		}

		reservation.Status = domain.ReservationCancelled
		reservation.ExpiresAt = nil
		reservation.UpdatedAt = s.clock.Now().UTC()
		if err := repos.Reservation.SaveReservation(ctx, *reservation); err != nil {
			return fmt.Errorf("save booked reservation cancellation: %w", err)
		}
		result = reservationResult(reservation)
		return nil
	})
	return result, err
}

func (s *Service) transition(ctx context.Context, reservationID domain.ReservationID, target domain.ReservationStatus) (ReservationResult, error) {
	if reservationID == "" {
		return ReservationResult{}, ErrInvalidRequest
	}
	var result ReservationResult
	err := s.transactions.Execute(ctx, func(ctx context.Context, repos repository.Repositories) error {
		reservation, err := repos.Reservation.LockByID(ctx, reservationID)
		if err != nil {
			return fmt.Errorf("lock reservation: %w", err)
		}
		if reservation.Status == target {
			result = reservationResult(reservation)
			return nil
		}
		if reservation.Status != domain.ReservationHeld {
			return ErrInvalidTransition
		}

		inventory, err := repos.Inventory.LockRange(ctx, reservation.HotelID, reservation.RoomTypeID, reservation.CheckIn, reservation.CheckOut)
		if err != nil {
			return fmt.Errorf("lock reservation inventory: %w", err)
		}
		if _, err := minimumAvailability(inventory, reservation.CheckIn, reservation.CheckOut); err != nil {
			return err
		}
		for i := range inventory {
			if inventory[i].HeldQuantity < reservation.Quantity {
				return ErrInvalidTransition
			}
			inventory[i].HeldQuantity -= reservation.Quantity
			if target == domain.ReservationBooked {
				inventory[i].BookedQuantity += reservation.Quantity
			}
			if err := repos.Inventory.SaveInventory(ctx, &inventory[i]); err != nil {
				return fmt.Errorf("save transition inventory date %s: %w", inventory[i].Date.Format(time.DateOnly), err)
			}
		}
		reservation.Status = target
		reservation.ExpiresAt = nil
		reservation.UpdatedAt = s.clock.Now().UTC()
		if err := repos.Reservation.SaveReservation(ctx, *reservation); err != nil {
			return fmt.Errorf("save reservation transition: %w", err)
		}
		result = reservationResult(reservation)
		return nil
	})
	return result, err
}

func validateRange(hotelID domain.HotelID, roomTypeID domain.RoomTypeID, checkIn, checkOut time.Time, quantity int) error {
	if hotelID == "" || roomTypeID == "" || quantity <= 0 || checkIn.IsZero() || !checkOut.After(checkIn) {
		return ErrInvalidRequest
	}
	if !sameDateBoundary(checkIn) || !sameDateBoundary(checkOut) {
		return ErrInvalidRequest
	}
	return nil
}

func sameDateBoundary(value time.Time) bool {
	return value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 && value.Nanosecond() == 0
}

func minimumAvailability(inventory []domain.Inventory, checkIn, checkOut time.Time) (int, error) {
	expected := int(checkOut.Sub(checkIn) / (24 * time.Hour))
	if expected <= 0 || len(inventory) != expected {
		return 0, ErrInventoryIncomplete
	}
	minimum := inventory[0].AvailableQuantity()
	expectedDate := checkIn
	for _, item := range inventory {
		if !item.Date.Equal(expectedDate) || item.AvailableQuantity() < 0 {
			return 0, ErrInventoryIncomplete
		}
		if available := item.AvailableQuantity(); available < minimum {
			minimum = available
		}
		expectedDate = expectedDate.AddDate(0, 0, 1)
	}
	return minimum, nil
}

func sameReservation(existing *domain.Reservation, input ReserveInventoryInput) bool {
	return existing != nil &&
		existing.BookingID == input.BookingID &&
		existing.HotelID == input.HotelID &&
		existing.RoomTypeID == input.RoomTypeID &&
		existing.CheckIn.Equal(input.CheckIn) &&
		existing.CheckOut.Equal(input.CheckOut) &&
		existing.Quantity == input.Quantity
}

func reservationResult(reservation *domain.Reservation) ReservationResult {
	return ReservationResult{
		ReservationID: reservation.ID,
		Status: reservation.Status,
		ExpiresAt: reservation.ExpiresAt,
	}
}

