package reminder

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type EntityType string

const (
	EntityApiary     EntityType = "apiary"
	EntityHive       EntityType = "hive"
	EntityInspection EntityType = "inspection"
	EntityHarvest    EntityType = "harvest"
)

func (e EntityType) Valid() bool {
	return e == EntityApiary || e == EntityHive || e == EntityInspection || e == EntityHarvest
}

type Status string

const (
	StatusScheduled  Status = "scheduled"
	StatusProcessing Status = "processing"
	StatusSent       Status = "sent"
	StatusCancelled  Status = "cancelled"
	StatusFailed     Status = "failed"
)

type Reminder struct {
	ID                   uuid.UUID  `json:"id"`
	UserID               uuid.UUID  `json:"user_id"`
	Title                string     `json:"title"`
	Note                 string     `json:"note"`
	EntityType           EntityType `json:"entity_type"`
	EntityID             uuid.UUID  `json:"entity_id"`
	ReminderType         string     `json:"reminder_type"`
	Source               string     `json:"source"`
	RemindAt             time.Time  `json:"remind_at"`
	Status               Status     `json:"status"`
	CancelReason         *string    `json:"cancel_reason,omitempty"`
	AttemptCount         int        `json:"attempt_count"`
	NextAttemptAt        time.Time  `json:"next_attempt_at"`
	LastError            *string    `json:"last_error,omitempty"`
	ProcessingToken      uuid.UUID  `json:"-"`
	ProcessingLeaseUntil *time.Time `json:"-"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

var ErrNotFound = errors.New("reminder not found")

type Filter struct {
	EntityType  *EntityType
	EntityID    *uuid.UUID
	Status      *Status
	Page, Limit int
}

type Repository interface {
	Create(context.Context, *Reminder) error
	Get(context.Context, uuid.UUID, uuid.UUID) (*Reminder, error)
	List(context.Context, uuid.UUID, Filter) ([]Reminder, int, error)
	Update(context.Context, *Reminder) error
	Delete(context.Context, uuid.UUID, uuid.UUID) error
	Cleanup(context.Context, []EntityRef) error
	ClaimDue(context.Context, time.Time, int, time.Duration) ([]Reminder, error)
	MarkSent(context.Context, uuid.UUID, uuid.UUID) error
	MarkCancelled(context.Context, uuid.UUID, uuid.UUID, string) error
	MarkRetry(context.Context, uuid.UUID, uuid.UUID, Status, int, time.Time, string) error
}

type EntityRef struct {
	Type EntityType `json:"type"`
	ID   uuid.UUID  `json:"id"`
}
