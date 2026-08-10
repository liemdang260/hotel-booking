package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/liemdang260/hotel-booking/services/availability/internal/domain/repository"
)

type recordingTransactionManager struct {
	called bool
	err    error
}

func (m *recordingTransactionManager) WithinTransaction(
	ctx context.Context,
	work repository.TransactionWork,
) error {
	m.called = true
	if m.err != nil {
		return m.err
	}
	return work(ctx, repository.Repositories{})
}

func TestTransactionBoundaryDelegatesOneUnitOfWork(t *testing.T) {
	manager := &recordingTransactionManager{}
	boundary := NewTransactionBoundary(manager)
	workCalled := false

	err := boundary.Execute(context.Background(), func(
		_ context.Context,
		_ repository.Repositories,
	) error {
		workCalled = true
		return nil
	})

	if err != nil {
		t.Fatalf("execute transaction: %v", err)
	}
	if !manager.called || !workCalled {
		t.Fatal("expected usecase work to run through transaction manager")
	}
}

func TestTransactionBoundaryRejectsNilWork(t *testing.T) {
	manager := &recordingTransactionManager{}
	boundary := NewTransactionBoundary(manager)

	err := boundary.Execute(context.Background(), nil)

	if !errors.Is(err, ErrTransactionWorkRequired) {
		t.Fatalf("expected ErrTransactionWorkRequired, got %v", err)
	}
	if manager.called {
		t.Fatal("transaction manager must not be called for nil work")
	}
}
