package reminder

import (
	"context"
	"encoding/json"
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
	UserID               uuid.UUID  `json:"userId"`
	Title                string     `json:"title"`
	Note                 string     `json:"note"`
	EntityType           EntityType `json:"entityType"`
	EntityID             uuid.UUID  `json:"entityId"`
	ReminderType         string     `json:"reminderType"`
	Source               string     `json:"source"`
	RemindAt             time.Time  `json:"remindAt"`
	Status               Status     `json:"status"`
	CancelReason         *string    `json:"cancelReason,omitempty"`
	AttemptCount         int        `json:"attemptCount"`
	NextAttemptAt        time.Time  `json:"nextAttemptAt"`
	LastError            *string    `json:"lastError,omitempty"`
	ProcessingToken      uuid.UUID  `json:"-"`
	ProcessingLeaseUntil *time.Time `json:"-"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

// MarshalJSON keeps all externally visible reminder instants canonical on the
// wire. The values remain absolute time.Time instants; this only controls the
// representation emitted by the API.
func (r Reminder) MarshalJSON() ([]byte, error) {
	type jsonReminder Reminder
	v := jsonReminder(r)
	v.RemindAt = r.RemindAt.UTC()
	v.NextAttemptAt = r.NextAttemptAt.UTC()
	v.CreatedAt = r.CreatedAt.UTC()
	v.UpdatedAt = r.UpdatedAt.UTC()
	return json.Marshal(v)
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
