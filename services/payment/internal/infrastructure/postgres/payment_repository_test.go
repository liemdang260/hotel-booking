package postgres

import (
	"errors"
	"testing"

	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

type duplicateError struct { constraint string }
func (e duplicateError) Error() string { return "duplicate key violates unique constraint " + e.constraint }
func (e duplicateError) SQLState() string { return "23505" }

func TestMapIdempotencyUniqueViolation(t *testing.T) {
	err := mapWriteError(duplicateError{"payments_idempotency_key_key"})
	if !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("err=%v", err)
	}
}

func TestMapBookingUniqueViolation(t *testing.T) {
	err := mapWriteError(duplicateError{"payments_booking_id_key"})
	if !errors.Is(err, repository.ErrBookingConflict) {
		t.Fatalf("err=%v", err)
	}
}
