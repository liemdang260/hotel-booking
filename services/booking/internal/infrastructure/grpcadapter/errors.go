package grpcadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/liemdang260/hotel-booking/services/booking/internal/repository"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func mapRPCError(ctx context.Context, err error, mutationMayHaveCommitted bool) error {
	if err == nil { return nil }
	if errors.Is(ctx.Err(), context.Canceled) { return context.Canceled }
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && mutationMayHaveCommitted {
		return fmt.Errorf("%w: caller deadline elapsed", repository.ErrOutcomeUnknown)
	}
	st, ok := status.FromError(err)
	if !ok { return fmt.Errorf("%w: %v", repository.ErrDownstreamUnavailable, err) }
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			if mapped := mapReason(info.Reason); mapped != nil {
				return fmt.Errorf("%w: %s", mapped, st.Message())
			}
		}
	}
	switch st.Code() {
	case codes.DeadlineExceeded:
		if mutationMayHaveCommitted { return fmt.Errorf("%w: %s", repository.ErrOutcomeUnknown, st.Message()) }
		return fmt.Errorf("%w: deadline exceeded", repository.ErrDownstreamUnavailable)
	case codes.Unavailable, codes.ResourceExhausted:
		if mutationMayHaveCommitted { return fmt.Errorf("%w: %s", repository.ErrOutcomeUnknown, st.Message()) }
		return fmt.Errorf("%w: %s", repository.ErrDownstreamUnavailable, st.Message())
	case codes.NotFound:
		return fmt.Errorf("%w: %s", repository.ErrInvalidRemoteResponse, st.Message())
	case codes.AlreadyExists:
		return fmt.Errorf("%w: %s", repository.ErrIdempotencyConflict, st.Message())
	default:
		return fmt.Errorf("%w: grpc code=%s message=%s", repository.ErrDownstreamUnavailable, st.Code(), st.Message())
	}
}
func mapReason(reason string) error {
	switch reason {
	case "QUOTE_NOT_FOUND": return repository.ErrQuoteNotFound
	case "QUOTE_EXPIRED": return repository.ErrQuoteExpired
	case "QUOTE_MISMATCH": return repository.ErrQuoteMismatch
	case "SOLD_OUT": return repository.ErrSoldOut
	case "INVENTORY_NOT_CONFIGURED": return repository.ErrInventoryNotConfigured
	case "RESERVATION_NOT_FOUND": return repository.ErrReservationNotFound
	case "RESERVATION_EXPIRED": return repository.ErrReservationExpired
	case "IDEMPOTENCY_CONFLICT": return repository.ErrIdempotencyConflict
	case "PAYMENT_DECLINED": return repository.ErrPaymentDeclined
	case "PAYMENT_NOT_FOUND": return repository.ErrPaymentNotFound
	case "PAYMENT_OUTCOME_UNKNOWN": return repository.ErrOutcomeUnknown
	default: return nil
	}
}
