package pushdevice

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

var ErrNotFound = errors.New("push device not found")

type Platform string

const (
	PlatformIOS     Platform = "ios"
	PlatformAndroid Platform = "android"
)

type PushDevice struct {
	ID, UserID           uuid.UUID
	Destination          string
	Platform             Platform
	CreatedAt, UpdatedAt time.Time
}

type Repository interface {
	Upsert(ctx context.Context, d *PushDevice) error
	FindByID(ctx context.Context, id, userID uuid.UUID) (*PushDevice, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error
}
