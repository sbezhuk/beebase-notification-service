package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
)

type PushDeviceRepository struct{ db Querier }

func NewPushDeviceRepository(db Querier) *PushDeviceRepository { return &PushDeviceRepository{db: db} }
func (r *PushDeviceRepository) Upsert(ctx context.Context, d *pushdevice.PushDevice) error {
	const q = `INSERT INTO push_devices (id,user_id,destination,platform,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (destination) DO UPDATE SET platform=EXCLUDED.platform, updated_at=EXCLUDED.updated_at WHERE push_devices.user_id=EXCLUDED.user_id RETURNING id, user_id, destination, platform, created_at, updated_at`
	var platform string
	if err := r.db.QueryRow(ctx, q, d.ID, d.UserID, d.Destination, string(d.Platform), d.CreatedAt, d.UpdatedAt).Scan(&d.ID, &d.UserID, &d.Destination, &platform, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pushdevice.ErrDestinationOwned
		}
		return fmt.Errorf("postgres: upsert push device: %w", err)
	}
	d.Platform = pushdevice.Platform(platform)
	return nil
}

func (r *PushDeviceRepository) Update(ctx context.Context, d *pushdevice.PushDevice) error {
	const q = `UPDATE push_devices SET destination=$3, platform=$4, updated_at=$5 WHERE id=$1 AND user_id=$2 RETURNING id,user_id,destination,platform,created_at,updated_at`
	var platform string
	if err := r.db.QueryRow(ctx, q, d.ID, d.UserID, d.Destination, string(d.Platform), d.UpdatedAt).Scan(&d.ID, &d.UserID, &d.Destination, &platform, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pushdevice.ErrNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return pushdevice.ErrDestinationOwned
		}
		return fmt.Errorf("postgres: update push device: %w", err)
	}
	d.Platform = pushdevice.Platform(platform)
	return nil
}
func (r *PushDeviceRepository) FindByID(ctx context.Context, id, userID uuid.UUID) (*pushdevice.PushDevice, error) {
	const q = `SELECT id,user_id,destination,platform,created_at,updated_at FROM push_devices WHERE id=$1 AND user_id=$2`
	var d pushdevice.PushDevice
	var p string
	if err := r.db.QueryRow(ctx, q, id, userID).Scan(&d.ID, &d.UserID, &d.Destination, &p, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pushdevice.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: find push device: %w", err)
	}
	d.Platform = pushdevice.Platform(p)
	return &d, nil
}
func (r *PushDeviceRepository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	const q = `DELETE FROM push_devices WHERE id=$1 AND user_id=$2`
	tag, err := r.db.Exec(ctx, q, id, userID)
	if err != nil {
		return fmt.Errorf("postgres: delete push device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pushdevice.ErrNotFound
	}
	return nil
}

func (r *PushDeviceRepository) DeleteByDestination(ctx context.Context, destination string) error {
	const q = `DELETE FROM push_devices WHERE destination=$1`
	if _, err := r.db.Exec(ctx, q, destination); err != nil {
		return fmt.Errorf("postgres: delete stale push device: %w", err)
	}
	return nil
}

func (r *PushDeviceRepository) DeleteAllByUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM push_devices WHERE user_id=$1`, userID)
	return err
}

func (r *PushDeviceRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]pushdevice.PushDevice, error) {
	rows, err := r.db.Query(ctx, `SELECT id,user_id,destination,platform,created_at,updated_at FROM push_devices WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pushdevice.PushDevice
	for rows.Next() {
		var d pushdevice.PushDevice
		var p string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Destination, &p, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.Platform = pushdevice.Platform(p)
		out = append(out, d)
	}
	return out, rows.Err()
}
