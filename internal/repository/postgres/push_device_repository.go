package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
)

type PushDeviceRepository struct{ db Querier }

func NewPushDeviceRepository(db Querier) *PushDeviceRepository { return &PushDeviceRepository{db: db} }
func (r *PushDeviceRepository) Upsert(ctx context.Context, d *pushdevice.PushDevice) error {
	const q = `INSERT INTO push_devices (id,user_id,destination,platform,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (destination) DO UPDATE SET user_id=EXCLUDED.user_id, platform=EXCLUDED.platform, updated_at=EXCLUDED.updated_at RETURNING id, user_id, destination, platform, created_at, updated_at`
	var platform string
	if err := r.db.QueryRow(ctx, q, d.ID, d.UserID, d.Destination, string(d.Platform), d.CreatedAt, d.UpdatedAt).Scan(&d.ID, &d.UserID, &d.Destination, &platform, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: upsert push device: %w", err)
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
