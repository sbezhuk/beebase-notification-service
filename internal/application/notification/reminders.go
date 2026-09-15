package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/reminder"
)

type EntityResolver interface {
	Exists(context.Context, reminder.EntityType, uuid.UUID) (bool, error)
}
type deviceLister interface {
	ListByUser(context.Context, uuid.UUID) ([]pushdevice.PushDevice, error)
}
type ReminderService struct {
	reminders   reminder.Repository
	devices     pushdevice.Repository
	sender      PushSender
	resolver    EntityResolver
	maxAttempts int
}

func NewReminderService(r reminder.Repository, d pushdevice.Repository, s PushSender, e EntityResolver) *ReminderService {
	return &ReminderService{reminders: r, devices: d, sender: s, resolver: e, maxAttempts: 5}
}

type CreateReminderInput struct {
	Title, Note string
	EntityType  reminder.EntityType
	EntityID    uuid.UUID
	RemindAt    time.Time
}

func (s *ReminderService) Create(ctx context.Context, user uuid.UUID, in CreateReminderInput) (*reminder.Reminder, error) {
	if strings.TrimSpace(in.Title) == "" || len(in.Title) > 200 {
		return nil, fmt.Errorf("title is required and must be at most 200 characters")
	}
	if len(in.Note) > 2000 {
		return nil, fmt.Errorf("note must be at most 2000 characters")
	}
	if !in.EntityType.Valid() {
		return nil, fmt.Errorf("invalid entity type")
	}
	if in.EntityID == uuid.Nil {
		return nil, fmt.Errorf("entity id is required")
	}
	if in.RemindAt.IsZero() {
		return nil, fmt.Errorf("remind_at is required")
	}
	now := time.Now().UTC()
	v := &reminder.Reminder{ID: uuid.New(), UserID: user, Title: strings.TrimSpace(in.Title), Note: in.Note, EntityType: in.EntityType, EntityID: in.EntityID, ReminderType: "custom", Source: "manual", RemindAt: in.RemindAt.UTC(), Status: reminder.StatusScheduled, AttemptCount: 0, NextAttemptAt: in.RemindAt.UTC(), CreatedAt: now, UpdatedAt: now}
	if err := s.reminders.Create(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}
func (s *ReminderService) Get(ctx context.Context, user, id uuid.UUID) (*reminder.Reminder, error) {
	return s.reminders.Get(ctx, id, user)
}
func (s *ReminderService) List(ctx context.Context, user uuid.UUID, f reminder.Filter) ([]reminder.Reminder, int, error) {
	return s.reminders.List(ctx, user, f)
}
func (s *ReminderService) Update(ctx context.Context, user, id uuid.UUID, in CreateReminderInput) (*reminder.Reminder, error) {
	v, err := s.Get(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Title) == "" || len(in.Title) > 200 || len(in.Note) > 2000 || !in.EntityType.Valid() || in.EntityID == uuid.Nil || in.RemindAt.IsZero() {
		return nil, fmt.Errorf("invalid reminder")
	}
	v.Title = strings.TrimSpace(in.Title)
	v.Note = in.Note
	v.EntityType = in.EntityType
	v.EntityID = in.EntityID
	v.RemindAt = in.RemindAt.UTC()
	v.NextAttemptAt = v.RemindAt
	v.Status = reminder.StatusScheduled
	v.CancelReason = nil
	v.LastError = nil
	v.UpdatedAt = time.Now().UTC()
	if err = s.reminders.Update(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}
func (s *ReminderService) Delete(ctx context.Context, user, id uuid.UUID) error {
	return s.reminders.Delete(ctx, id, user)
}
func (s *ReminderService) Cleanup(ctx context.Context, es []reminder.EntityRef) error {
	return s.reminders.Cleanup(ctx, es)
}

func (s *ReminderService) ProcessDue(ctx context.Context, now time.Time, batch int) error {
	items, err := s.reminders.ClaimDue(ctx, now.UTC(), batch)
	if err != nil {
		return err
	}
	for _, v := range items {
		if err := s.processOne(ctx, v, now.UTC()); err != nil {
			continue
		}
	}
	return nil
}
func (s *ReminderService) processOne(ctx context.Context, v reminder.Reminder, now time.Time) error {
	exists, err := s.resolver.Exists(ctx, v.EntityType, v.EntityID)
	if err != nil {
		return s.retry(ctx, v, err, now)
	}
	if !exists {
		return s.reminders.MarkCancelled(ctx, v.ID, "entity_deleted")
	}
	lister, ok := s.devices.(deviceLister)
	if !ok {
		return s.retry(ctx, v, fmt.Errorf("device repository cannot list devices"), now)
	}
	devices, err := lister.ListByUser(ctx, v.UserID)
	if err != nil {
		return s.retry(ctx, v, err, now)
	}
	success := 0
	var last error
	body := v.Note
	if strings.TrimSpace(body) == "" {
		body = v.Title
	}
	for _, d := range devices {
		err = s.sender.Send(ctx, PushMessage{Destination: d.Destination, Title: v.Title, Body: body, Data: map[string]string{"type": "reminder", "reminder_id": v.ID.String(), "entity_type": string(v.EntityType), "entity_id": v.EntityID.String()}})
		if err == nil {
			success++
			continue
		}
		last = err
		var de *DeliveryError
		if errors.As(err, &de) && (de.Kind == DeliveryInvalidDestination || de.Kind == DeliveryUnregistered) {
			_ = s.devices.DeleteByDestination(ctx, d.Destination)
		}
	}
	if success > 0 {
		return s.reminders.MarkSent(ctx, v.ID)
	}
	if last == nil {
		last = fmt.Errorf("user has no registered push devices")
	}
	return s.retry(ctx, v, last, now)
}
func (s *ReminderService) retry(ctx context.Context, v reminder.Reminder, err error, now time.Time) error {
	attempt := v.AttemptCount + 1
	safe := err.Error()
	if len(safe) > 500 {
		safe = safe[:500]
	}
	if attempt >= s.maxAttempts {
		return s.reminders.MarkRetry(ctx, v.ID, reminder.StatusFailed, attempt, now.Add(24*time.Hour), safe)
	}
	backoff := time.Duration(1<<min(attempt, 6)) * time.Minute
	return s.reminders.MarkRetry(ctx, v.ID, reminder.StatusScheduled, attempt, now.Add(backoff), safe)
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
