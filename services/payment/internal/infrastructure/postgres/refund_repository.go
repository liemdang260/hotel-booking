package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/liemdang260/hotel-booking/services/payment/internal/domain"
	"github.com/liemdang260/hotel-booking/services/payment/internal/repository"
)

const refundColumns = "id,payment_id,booking_id,idempotency_key,amount_minor,currency,status,provider_reference,failure_code,created_at,updated_at"

func scanRefund(s interface{ Scan(...any) error }) (domain.Refund, error) {
	var r domain.Refund
	err := s.Scan(&r.ID, &r.PaymentID, &r.BookingID, &r.IdempotencyKey, &r.AmountMinor,
		&r.Currency, &r.Status, &r.ProviderReference, &r.FailureCode, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (r *PaymentRepository) CreateRefund(ctx context.Context, refund domain.Refund) (domain.Refund, error) {
	row := r.db.QueryRowContext(ctx, `INSERT INTO refunds (`+refundColumns+`)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+refundColumns,
		refund.ID, refund.PaymentID, refund.BookingID, refund.IdempotencyKey, refund.AmountMinor,
		refund.Currency, refund.Status, refund.ProviderReference, refund.FailureCode, refund.CreatedAt, refund.UpdatedAt)
	got, err := scanRefund(row)
	if err != nil {
		var state sqlStateError
		if errors.As(err, &state) && state.SQLState() == "23505" &&
			strings.Contains(err.Error(), "refunds_idempotency_key_key") {
			return domain.Refund{}, repository.ErrRefundIdempotencyConflict
		}
		return domain.Refund{}, fmt.Errorf("create refund: %w", err)
	}
	return got, nil
}

func (r *PaymentRepository) GetRefundByID(ctx context.Context, id string) (domain.Refund, error) {
	got, err := scanRefund(r.db.QueryRowContext(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Refund{}, repository.ErrRefundNotFound
	}
	return got, err
}

func (r *PaymentRepository) GetRefundByIdempotencyKey(ctx context.Context, key string) (domain.Refund, error) {
	got, err := scanRefund(r.db.QueryRowContext(ctx, "SELECT "+refundColumns+" FROM refunds WHERE idempotency_key=$1", key))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Refund{}, repository.ErrRefundNotFound
	}
	return got, err
}

func (r *PaymentRepository) BeginRefundAttempt(ctx context.Context, refundID string, a domain.RefundAttempt, now time.Time) (domain.Refund, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Refund{}, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE refunds SET status='PROCESSING',updated_at=$2
WHERE id=$1 AND status IN ('PENDING','UNKNOWN')`, refundID, now)
	if err != nil {
		return domain.Refund{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return domain.Refund{}, repository.ErrConcurrentUpdate
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO refund_attempts
(id,refund_id,outcome,started_at) VALUES ($1,$2,$3,$4)`, a.ID, refundID, a.Outcome, a.StartedAt)
	if err != nil {
		return domain.Refund{}, err
	}
	got, err := scanRefund(tx.QueryRowContext(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", refundID))
	if err != nil {
		return domain.Refund{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Refund{}, err
	}
	return got, nil
}

func (r *PaymentRepository) CompleteRefundAttempt(ctx context.Context, refundID, attemptID string,
	out domain.AttemptOutcome, status domain.RefundStatus, requestRef, providerRef, failure, raw string,
	now time.Time) (domain.Refund, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Refund{}, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE refund_attempts SET outcome=$3,provider_request_ref=$4,
provider_reference=$5,failure_code=$6,raw_outcome=$7,finished_at=$8
WHERE id=$1 AND refund_id=$2 AND finished_at IS NULL`,
		attemptID, refundID, out, requestRef, providerRef, failure, raw, now)
	if err != nil {
		return domain.Refund{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return domain.Refund{}, repository.ErrConcurrentUpdate
	}
	res, err = tx.ExecContext(ctx, `UPDATE refunds SET status=$2,provider_reference=$3,
failure_code=$4,updated_at=$5 WHERE id=$1 AND status='PROCESSING'`,
		refundID, status, providerRef, failure, now)
	if err != nil {
		return domain.Refund{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return domain.Refund{}, repository.ErrConcurrentUpdate
	}
	got, err := scanRefund(tx.QueryRowContext(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", refundID))
	if err != nil {
		return domain.Refund{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Refund{}, err
	}
	return got, nil
}

func (r *PaymentRepository) ResolveUnknownRefund(ctx context.Context, id string, status domain.RefundStatus,
	providerRef, failure string, now time.Time) (domain.Refund, error) {
	row := r.db.QueryRowContext(ctx, `UPDATE refunds SET status=$2,provider_reference=$3,
failure_code=$4,updated_at=$5 WHERE id=$1 AND status='UNKNOWN' RETURNING `+refundColumns,
		id, status, providerRef, failure, now)
	got, err := scanRefund(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Refund{}, repository.ErrConcurrentUpdate
	}
	return got, err
}

type RefundRepository struct{ payments *PaymentRepository }

func NewRefundRepository(db *sql.DB) *RefundRepository {
	return &RefundRepository{payments: NewPaymentRepository(db)}
}

func (r *RefundRepository) Create(ctx context.Context, v domain.Refund) (domain.Refund, error) {
	return r.payments.CreateRefund(ctx, v)
}
func (r *RefundRepository) GetByID(ctx context.Context, id string) (domain.Refund, error) {
	return r.payments.GetRefundByID(ctx, id)
}
func (r *RefundRepository) GetByIdempotencyKey(ctx context.Context, key string) (domain.Refund, error) {
	return r.payments.GetRefundByIdempotencyKey(ctx, key)
}
func (r *RefundRepository) BeginAttempt(ctx context.Context, id string, a domain.RefundAttempt, now time.Time) (domain.Refund, error) {
	return r.payments.BeginRefundAttempt(ctx, id, a, now)
}
func (r *RefundRepository) CompleteAttempt(ctx context.Context, id, attemptID string, out domain.AttemptOutcome,
	status domain.RefundStatus, requestRef, providerRef, failure, raw string, now time.Time) (domain.Refund, error) {
	return r.payments.CompleteRefundAttempt(ctx, id, attemptID, out, status, requestRef, providerRef, failure, raw, now)
}
func (r *RefundRepository) ResolveUnknown(ctx context.Context, id string, status domain.RefundStatus,
	providerRef, failure string, now time.Time) (domain.Refund, error) {
	return r.payments.ResolveUnknownRefund(ctx, id, status, providerRef, failure, now)
}
