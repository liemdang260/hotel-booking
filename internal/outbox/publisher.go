package outbox

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidConfiguration = errors.New("outbox: invalid publisher configuration")

type Event struct {
	ID               string
	AggregateType    string
	AggregateID      string
	AggregateVersion int64
	EventType        string
	EventVersion     int
	Payload          []byte
	CorrelationID    string
	CausationID      string
	OccurredAt       time.Time
	ClaimToken       string
	Attempt          int
}

type ClaimRequest struct {
	Limit       int
	ClaimToken  string
	Now         time.Time
	LeaseUntil  time.Time
}

type Repository interface {
	Claim(context.Context, ClaimRequest) ([]Event, error)
	MarkPublished(context.Context, string, string, time.Time) error
	MarkRetry(context.Context, string, string, time.Time, string) error
}

type EventPublisher interface {
	Publish(context.Context, Event) error
}

type Clock interface { Now() time.Time }

type Backoff func(attempt int) time.Duration

type Config struct {
	MaxBatchSize  int
	LeaseDuration time.Duration
	Backoff       Backoff
}

type Result struct {
	Claimed   int
	Published int
	Retried   int
}

type PublishBatch struct {
	repository Repository
	publisher  EventPublisher
	clock      Clock
	config     Config
}

func NewPublishBatch(repository Repository, publisher EventPublisher, clock Clock, config Config) (*PublishBatch, error) {
	if repository == nil || publisher == nil || clock == nil || config.MaxBatchSize <= 0 ||
		config.LeaseDuration <= 0 || config.Backoff == nil {
		return nil, ErrInvalidConfiguration
	}
	return &PublishBatch{repository: repository, publisher: publisher, clock: clock, config: config}, nil
}

func (u *PublishBatch) ExecuteBatch(ctx context.Context, requested int) (Result, error) {
	if requested <= 0 {
		return Result{}, fmt.Errorf("%w: batch size must be positive", ErrInvalidConfiguration)
	}
	limit := requested
	if limit > u.config.MaxBatchSize {
		limit = u.config.MaxBatchSize
	}
	now := u.clock.Now().UTC()
	token, err := newClaimToken()
	if err != nil {
		return Result{}, err
	}
	events, err := u.repository.Claim(ctx, ClaimRequest{
		Limit: limit, ClaimToken: token, Now: now, LeaseUntil: now.Add(u.config.LeaseDuration),
	})
	if err != nil {
		return Result{}, fmt.Errorf("outbox: claim events: %w", err)
	}
	if len(events) > limit {
		return Result{}, fmt.Errorf("outbox: repository returned %d events for limit %d", len(events), limit)
	}

	result := Result{Claimed: len(events)}
	var failures []error
	for _, event := range events {
		if event.ClaimToken == "" {
			event.ClaimToken = token
		}
		if event.ClaimToken != token {
			failures = append(failures, fmt.Errorf("outbox event %s: claim token mismatch", event.ID))
			continue
		}
		if err := u.publisher.Publish(ctx, event); err != nil {
			retryAt := u.clock.Now().UTC().Add(u.config.Backoff(event.Attempt + 1))
			if retryErr := u.repository.MarkRetry(ctx, event.ID, token, retryAt, err.Error()); retryErr != nil {
				failures = append(failures, fmt.Errorf("outbox event %s: publish: %v; record retry: %w", event.ID, err, retryErr))
			} else {
				result.Retried++
				failures = append(failures, fmt.Errorf("outbox event %s: publish: %w", event.ID, err))
			}
			continue
		}
		if err := u.repository.MarkPublished(ctx, event.ID, token, u.clock.Now().UTC()); err != nil {
			failures = append(failures, fmt.Errorf("outbox event %s: record publication: %w", event.ID, err))
			continue
		}
		result.Published++
	}
	return result, errors.Join(failures...)
}

func DefaultBackoff(attempt int) time.Duration {
	if attempt < 1 { attempt = 1 }
	delays := [...]time.Duration{time.Second, 5*time.Second, 30*time.Second, 2*time.Minute, 10*time.Minute}
	if attempt > len(delays) { return delays[len(delays)-1] }
	return delays[attempt-1]
}

func newClaimToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("outbox: generate claim token: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
