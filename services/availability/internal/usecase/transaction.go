package usecase

import (
	"context"
	"errors"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain/repository"
)

var ErrTransactionWorkRequired = errors.New("usecase: transaction work is required")

// TransactionBoundary is injected into mutation usecases. A usecase chooses
// when a transaction begins and which repository operations belong to it;
// infrastructure owns only the database-specific begin/commit/rollback work.
type TransactionBoundary struct {
	transactions repository.TransactionManager
}

func NewTransactionBoundary(transactions repository.TransactionManager) TransactionBoundary {
	return TransactionBoundary{transactions: transactions}
}

func (b TransactionBoundary) Execute(ctx context.Context, work repository.TransactionWork) error {
	if work == nil {
		return ErrTransactionWorkRequired
	}
	return b.transactions.WithinTransaction(ctx, work)
}
