package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type ExpirationUsecase interface {
	ExecuteBatch(context.Context, int) (int, error)
}

type Expiration struct {
	usecase     ExpirationUsecase
	concurrency int
	batchSize   int
}

func NewExpiration(usecase ExpirationUsecase, concurrency, batchSize int) (*Expiration, error) {
	if usecase == nil || concurrency <= 0 || batchSize <= 0 {
		return nil, errors.New("expiration worker: usecase, concurrency, and batch size are required")
	}
	return &Expiration{usecase: usecase, concurrency: concurrency, batchSize: batchSize}, nil
}

// RunOnce starts at most the configured number of transaction workers.
// Each worker claims a disjoint batch through PostgreSQL SKIP LOCKED.
func (w *Expiration) RunOnce(ctx context.Context) (int, error) {
	type result struct {
		count int
		err error
	}
	results := make(chan result, w.concurrency)
	var group sync.WaitGroup
	for i := 0; i < w.concurrency; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			count, err := w.usecase.ExecuteBatch(ctx, w.batchSize)
			results <- result{count:count, err:err}
		}()
	}
	group.Wait()
	close(results)

	total := 0
	var failures []error
	for result := range results {
		total += result.count
		if result.err != nil {
			failures = append(failures, result.err)
		}
	}
	if len(failures) > 0 {
		return total, fmt.Errorf("expiration worker: %w", errors.Join(failures...))
	}
	return total, nil
}
