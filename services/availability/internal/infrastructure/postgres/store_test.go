package postgres

import (
	"strings"
	"testing"
)

func TestInventoryLockUsesStableDateOrderBeforeForUpdate(t *testing.T) {
	orderPosition := strings.Index(lockInventoryRangeSQL, "ORDER BY inventory_date")
	lockPosition := strings.Index(lockInventoryRangeSQL, "FOR UPDATE")

	if orderPosition < 0 || lockPosition < 0 {
		t.Fatal("inventory lock query must contain ORDER BY inventory_date and FOR UPDATE")
	}
	if orderPosition > lockPosition {
		t.Fatal("inventory lock query must establish stable date order before locking")
	}
	if !strings.Contains(lockInventoryRangeSQL, "inventory_date >= $3") ||
		!strings.Contains(lockInventoryRangeSQL, "inventory_date < $4") {
		t.Fatal("inventory lock query must use check-in inclusive/check-out exclusive dates")
	}
}

func TestReservationMutationLocksReservationRow(t *testing.T) {
	if !strings.Contains(lockReservationByIDSQL, "WHERE id = $1") ||
		!strings.Contains(lockReservationByIDSQL, "FOR UPDATE") {
		t.Fatal("reservation mutation lookup must lock the reservation row")
	}
}

func TestRecordedInventoryDatesAreReturnedInStableOrder(t *testing.T) {
	if !strings.Contains(listReservationInventorySQL, "ORDER BY inventory_date") {
		t.Fatal("reservation inventory must be read in stable date order")
	}
}
