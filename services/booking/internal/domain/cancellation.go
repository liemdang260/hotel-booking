package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidCancellation = errors.New("invalid booking cancellation")
	ErrBookingNotCancellable = errors.New("booking not cancellable")
	ErrCancellationIdempotencyConflict = errors.New("cancellation idempotency conflict")
	ErrCancellationAlreadyActive = errors.New("cancellation already active")
)

type CancellationState string

const (
	CancellationStarted CancellationState = "STARTED"
	CancellationPolicyApproved CancellationState = "POLICY_APPROVED"
	CancellationCancellingReservation CancellationState = "CANCELLING_RESERVATION"
	CancellationReservationCancelled CancellationState = "RESERVATION_CANCELLED"
	CancellationRefundProcessing CancellationState = "REFUND_PROCESSING"
	CancellationRefundUnknown CancellationState = "REFUND_UNKNOWN"
	CancellationCompleted CancellationState = "COMPLETED"
	CancellationPolicyRejected CancellationState = "POLICY_REJECTED"
	CancellationFailed CancellationState = "FAILED"
)

type BookingCancellation struct {
	ID, BookingID, IdempotencyKey, RequestHash, Reason string
	State CancellationState
	PolicyEvaluatedAt time.Time
	RefundAmountMinor int64
	Currency, RefundID, FailureCode string
	RetryCount int
	NextRetryAt *time.Time
	Version int64
	CreatedAt, UpdatedAt time.Time
}

func NewBookingCancellation(id, bookingID, key, hash, reason, currency string, evaluatedAt time.Time, refund int64) (BookingCancellation, error) {
	if strings.TrimSpace(id)=="" || strings.TrimSpace(bookingID)=="" || strings.TrimSpace(key)=="" ||
		len(strings.TrimSpace(hash))!=64 || len(strings.TrimSpace(currency))!=3 || evaluatedAt.IsZero() || refund<0 {
		return BookingCancellation{}, ErrInvalidCancellation
	}
	now:=evaluatedAt.UTC()
	return BookingCancellation{ID:id,BookingID:bookingID,IdempotencyKey:key,RequestHash:hash,Reason:reason,
		State:CancellationPolicyApproved,PolicyEvaluatedAt:now,RefundAmountMinor:refund,
		Currency:strings.ToUpper(currency),Version:1,CreatedAt:now,UpdatedAt:now},nil
}

func (c BookingCancellation) SameRequest(hash string) bool { return c.RequestHash==hash }
func (c BookingCancellation) Terminal() bool {
	return c.State==CancellationCompleted || c.State==CancellationPolicyRejected || c.State==CancellationFailed
}
