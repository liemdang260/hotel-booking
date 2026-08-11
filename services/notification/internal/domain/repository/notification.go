package repository

import (
	"context"
	"time"
)

type IntegrationEvent struct {
	ID        string
	Type      string
	Version   int
	Payload   []byte
	CreatedAt time.Time
}

type NotificationJob struct {
	EventID string
	Kind    string
	Payload []byte
}

type Inbox interface {
	RecordEventAndJob(context.Context, IntegrationEvent, NotificationJob) (created bool, err error)
}
