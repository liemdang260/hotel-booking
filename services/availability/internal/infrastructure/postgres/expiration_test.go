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
		"FOR UPDATE SKIP LOCKED",
		"LIMIT $2",
	}
	for _, fragment := range required {
		if !strings.Contains(lockExpiredReservationsSQL, fragment) {
			t.Fatalf("expiration claim query missing %q", fragment)
		}
	}
	if strings.Index(lockExpiredReservationsSQL, "ORDER BY expires_at, id") >
		strings.Index(lockExpiredReservationsSQL, "FOR UPDATE SKIP LOCKED") {
		t.Fatal("expiration reservations must be ordered before rows are locked")
	}
}
