package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sbezhuk/beebase-notification-service/internal/domain/reminder"
)

type ReminderRepository struct{ db *pgxpool.Pool }

func NewReminderRepository(db *pgxpool.Pool) *ReminderRepository { return &ReminderRepository{db: db} }

const reminderColumns = `id,user_id,title,note,entity_type,entity_id,reminder_type,source,remind_at,status,cancel_reason,attempt_count,next_attempt_at,last_error,processing_token,processing_lease_until,created_at,updated_at`

func scanReminder(s pgx.Row, r *reminder.Reminder) error {
	return s.Scan(&r.ID, &r.UserID, &r.Title, &r.Note, &r.EntityType, &r.EntityID, &r.ReminderType, &r.Source, &r.RemindAt, &r.Status, &r.CancelReason, &r.AttemptCount, &r.NextAttemptAt, &r.LastError, &r.ProcessingToken, &r.ProcessingLeaseUntil, &r.CreatedAt, &r.UpdatedAt)
}
func scanReminderRows(rows pgx.Rows) (reminder.Reminder, error) {
	var r reminder.Reminder
	err := rows.Scan(&r.ID, &r.UserID, &r.Title, &r.Note, &r.EntityType, &r.EntityID, &r.ReminderType, &r.Source, &r.RemindAt, &r.Status, &r.CancelReason, &r.AttemptCount, &r.NextAttemptAt, &r.LastError, &r.ProcessingToken, &r.ProcessingLeaseUntil, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}
func (r *ReminderRepository) Create(ctx context.Context, v *reminder.Reminder) error {
	_, err := r.db.Exec(ctx, `INSERT INTO reminders (id,user_id,title,note,entity_type,entity_id,reminder_type,source,remind_at,status,cancel_reason,attempt_count,next_attempt_at,last_error,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, v.ID, v.UserID, v.Title, v.Note, v.EntityType, v.EntityID, v.ReminderType, v.Source, v.RemindAt, v.Status, v.CancelReason, v.AttemptCount, v.NextAttemptAt, v.LastError, v.CreatedAt, v.UpdatedAt)
	return err
}
func (r *ReminderRepository) Get(ctx context.Context, id, user uuid.UUID) (*reminder.Reminder, error) {
	var v reminder.Reminder
	err := scanReminder(r.db.QueryRow(ctx, `SELECT `+reminderColumns+` FROM reminders WHERE id=$1 AND user_id=$2`, id, user), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, reminder.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}
func (r *ReminderRepository) List(ctx context.Context, user uuid.UUID, f reminder.Filter) ([]reminder.Reminder, int, error) {
	where := []string{"user_id=$1"}
	args := []any{user}
	n := 2
	if f.EntityType != nil {
		where = append(where, fmt.Sprintf("entity_type=$%d", n))
		args = append(args, *f.EntityType)
		n++
	}
	if f.EntityID != nil {
		where = append(where, fmt.Sprintf("entity_id=$%d", n))
		args = append(args, *f.EntityID)
		n++
	}
	if len(f.Statuses) > 0 {
		placeholders := make([]string, len(f.Statuses))
		for i, s := range f.Statuses {
			placeholders[i] = fmt.Sprintf("$%d", n)
			args = append(args, s)
			n++
		}
		where = append(where, fmt.Sprintf("status IN (%s)", strings.Join(placeholders, ",")))
	} else if f.Status != nil {
		where = append(where, fmt.Sprintf("status=$%d", n))
		args = append(args, *f.Status)
		n++
	}
	count := 0
	if err := r.db.QueryRow(ctx, "SELECT count(*) FROM reminders WHERE "+strings.Join(where, " AND "), args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Page*f.Limit-f.Limit)
	rows, err := r.db.Query(ctx, "SELECT "+reminderColumns+" FROM reminders WHERE "+strings.Join(where, " AND ")+fmt.Sprintf(" ORDER BY remind_at DESC LIMIT $%d OFFSET $%d", n, n+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []reminder.Reminder{}
	for rows.Next() {
		v, e := scanReminderRows(rows)
		if e != nil {
			return nil, 0, e
		}
		out = append(out, v)
	}
	return out, count, rows.Err()
}
func (r *ReminderRepository) Update(ctx context.Context, v *reminder.Reminder) error {
	tag, err := r.db.Exec(ctx, `UPDATE reminders SET title=$3,note=$4,entity_type=$5,entity_id=$6,remind_at=$7,status=$8,updated_at=$9,next_attempt_at=$10,processing_token=NULL,processing_lease_until=NULL WHERE id=$1 AND user_id=$2`, v.ID, v.UserID, v.Title, v.Note, v.EntityType, v.EntityID, v.RemindAt, v.Status, v.UpdatedAt, v.NextAttemptAt)
	if err == nil && tag.RowsAffected() == 0 {
		err = reminder.ErrNotFound
	}
	return err
}
func (r *ReminderRepository) Delete(ctx context.Context, id, user uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM reminders WHERE id=$1 AND user_id=$2`, id, user)
	if err == nil && tag.RowsAffected() == 0 {
		err = reminder.ErrNotFound
	}
	return err
}
func (r *ReminderRepository) Cleanup(ctx context.Context, es []reminder.EntityRef) error {
	for _, e := range es {
		if _, err := r.db.Exec(ctx, `DELETE FROM reminders WHERE entity_type=$1 AND entity_id=$2`, e.Type, e.ID); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReminderRepository) DeleteAllByUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM reminders WHERE user_id=$1`, userID)
	return err
}
func (r *ReminderRepository) ClaimDue(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]reminder.Reminder, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT `+reminderColumns+` FROM reminders WHERE (status='scheduled' AND next_attempt_at <= $1 AND remind_at <= $1) OR (status='processing' AND processing_lease_until <= $1) ORDER BY remind_at LIMIT $2 FOR UPDATE SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, err
	}
	var out []reminder.Reminder
	for rows.Next() {
		v, e := scanReminderRows(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, v)
	}
	rows.Close()
	for i := range out {
		out[i].ProcessingToken = uuid.New()
		leaseUntil := now.Add(lease)
		out[i].ProcessingLeaseUntil = &leaseUntil
		if _, err = tx.Exec(ctx, `UPDATE reminders SET status='processing',processing_token=$2,processing_lease_until=$3,updated_at=$4 WHERE id=$1`, out[i].ID, out[i].ProcessingToken, leaseUntil, now); err != nil {
			return nil, err
		}
		out[i].Status = reminder.StatusProcessing
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
func (r *ReminderRepository) MarkSent(ctx context.Context, id, token uuid.UUID) error {
	_, e := r.db.Exec(ctx, `UPDATE reminders SET status='sent',processing_token=NULL,processing_lease_until=NULL,updated_at=$3 WHERE id=$1 AND status='processing' AND processing_token=$2`, id, token, time.Now().UTC())
	return e
}
func (r *ReminderRepository) MarkCancelled(ctx context.Context, id, token uuid.UUID, reason string) error {
	_, e := r.db.Exec(ctx, `UPDATE reminders SET status='cancelled',cancel_reason=$3,processing_token=NULL,processing_lease_until=NULL,updated_at=$4 WHERE id=$1 AND status='processing' AND processing_token=$2`, id, token, reason, time.Now().UTC())
	return e
}
func (r *ReminderRepository) MarkRetry(ctx context.Context, id, token uuid.UUID, status reminder.Status, attempt int, next time.Time, last string) error {
	_, e := r.db.Exec(ctx, `UPDATE reminders SET status=$3,attempt_count=$4,next_attempt_at=$5,last_error=$6,processing_token=NULL,processing_lease_until=NULL,updated_at=$7 WHERE id=$1 AND status='processing' AND processing_token=$2`, id, token, status, attempt, next, last, time.Now().UTC())
	return e
}
