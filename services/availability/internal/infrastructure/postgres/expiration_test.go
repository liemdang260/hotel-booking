package postgres

import (
	"strings"
	"testing"
)

func TestExpirationClaimUsesSkipLockedAndStableOrder(t *testing.T) {
	required := []string{
		"WHERE status = 'HELD'",
		"expires_at <= $1",
		"ORDER BY expires_at, id",
		"LIMIT $2",
		"FOR UPDATE SKIP LOCKED",
	}
	for _, fragment := range required {
		if !strings.Contains(lockExpiredReservationsSQL, fragment) {
			t.Fatalf("expiration claim query missing %q", fragment)
		}
	}
	orderPosition := strings.Index(lockExpiredReservationsSQL, "ORDER BY expires_at, id")
	limitPosition := strings.Index(lockExpiredReservationsSQL, "LIMIT $2")
	lockPosition := strings.Index(lockExpiredReservationsSQL, "FOR UPDATE SKIP LOCKED")
	if !(orderPosition < limitPosition && limitPosition < lockPosition) {
		t.Fatal("expiration query must order and bound candidates before applying the locking clause")
	}
}
