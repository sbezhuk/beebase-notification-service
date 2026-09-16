package pushdevice

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

var ErrNotFound = errors.New("push device not found")
var ErrDestinationOwned = errors.New("push destination belongs to another user")
var ErrInactiveSession = errors.New("authentication session is no longer active")
var ErrStaleSession = errors.New("registration belongs to an older session")

type Platform string

const (
	PlatformIOS     Platform = "ios"
	PlatformAndroid Platform = "android"
)

type PushDevice struct {
	ID, UserID, SessionID uuid.UUID
	SessionGeneration     int64
	Destination           string
	Platform              Platform
	CreatedAt, UpdatedAt  time.Time
}

type Repository interface {
	Upsert(ctx context.Context, d *PushDevice) error
	Update(ctx context.Context, d *PushDevice) error
	FindByID(ctx context.Context, id, userID uuid.UUID) (*PushDevice, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error
	DeleteByDestination(ctx context.Context, destination string) error
	DeleteAllByUser(ctx context.Context, userID uuid.UUID) error
	DeleteBySession(ctx context.Context, userID, sessionID uuid.UUID) error
}
