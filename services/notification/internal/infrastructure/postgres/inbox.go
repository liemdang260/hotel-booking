package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/liemdang260/hotel-booking/services/notification/internal/domain/repository"
)

type Inbox struct {
	db *sql.DB
}

func NewInbox(db *sql.DB) (*Inbox, error) {
	if db == nil {
		return nil, errors.New("notification postgres: database is required")
	}
	return &Inbox{db: db}, nil
}

func (s *Inbox) RecordEventAndJob(ctx context.Context, event repository.IntegrationEvent, job repository.NotificationJob) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin notification inbox transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `INSERT INTO notification_processed_events
		(event_id,event_type,event_version,processed_at)
		VALUES($1::uuid,$2,$3,$4)
		ON CONFLICT(event_id) DO NOTHING`, event.ID, event.Type, event.Version, event.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("record processed event: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read processed event result: %w", err)
	}
	if rows == 0 {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit duplicate event: %w", err)
		}
		return false, nil
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_jobs
		(event_id,kind,payload,status,created_at)
		VALUES($1::uuid,$2,$3,'PENDING',$4)`, job.EventID, job.Kind, job.Payload, event.CreatedAt); err != nil {
		return false, fmt.Errorf("create notification job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit notification event: %w", err)
	}
	return true, nil
}

var _ repository.Inbox = (*Inbox)(nil)
