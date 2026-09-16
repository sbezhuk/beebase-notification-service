package notification

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
	"log/slog"
	"strings"
	"time"
)

type Service struct {
	devices pushdevice.Repository
	sender  PushSender
}

func NewService(devices pushdevice.Repository, sender PushSender) *Service {
	return &Service{devices: devices, sender: sender}
}

func (s *Service) RegisterDevice(ctx context.Context, userID uuid.UUID, destination string, platform pushdevice.Platform) (*pushdevice.PushDevice, error) {
	if err := validateDevice(destination, platform); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	d := &pushdevice.PushDevice{ID: uuid.New(), UserID: userID, Destination: destination, Platform: platform, CreatedAt: now, UpdatedAt: now}
	if err := s.devices.Upsert(ctx, d); err != nil {
		return nil, fmt.Errorf("register device: %w", err)
	}
	return d, nil
}

func (s *Service) UpdateDevice(ctx context.Context, userID, id uuid.UUID, destination string, platform pushdevice.Platform) (*pushdevice.PushDevice, error) {
	if err := validateDevice(destination, platform); err != nil {
		return nil, err
	}
	d := &pushdevice.PushDevice{ID: id, UserID: userID, Destination: destination, Platform: platform, UpdatedAt: time.Now().UTC()}
	if err := s.devices.Update(ctx, d); err != nil {
		return nil, fmt.Errorf("update device: %w", err)
	}
	return d, nil
}

func validateDevice(destination string, platform pushdevice.Platform) error {
	if strings.TrimSpace(destination) == "" {
		return fmt.Errorf("destination is required")
	}
	if platform != pushdevice.PlatformIOS && platform != pushdevice.PlatformAndroid {
		return fmt.Errorf("platform must be ios or android")
	}
	return nil
}
func (s *Service) RemoveDevice(ctx context.Context, id, userID uuid.UUID) error {
	if err := s.devices.Delete(ctx, id, userID); err != nil {
		return fmt.Errorf("remove device: %w", err)
	}
	return nil
}

func (s *Service) DeleteAllByUser(ctx context.Context, userID uuid.UUID) error {
	return s.devices.DeleteAllByUser(ctx, userID)
}
func (s *Service) Send(ctx context.Context, m PushMessage) error {
	if strings.TrimSpace(m.Destination) == "" {
		return fmt.Errorf("destination is required")
	}
	if strings.TrimSpace(m.Title) == "" || strings.TrimSpace(m.Body) == "" {
		return fmt.Errorf("title and body are required")
	}
	err := s.sender.Send(ctx, m)
	var deliveryErr *DeliveryError
	if errors.As(err, &deliveryErr) && (deliveryErr.Kind == DeliveryInvalidDestination || deliveryErr.Kind == DeliveryUnregistered) {
		// A definitive provider response means this destination cannot be used
		// again. Cleanup is deliberately not attempted for transient failures.
		deleteStaleDevice(ctx, s.devices, m.Destination, deliveryErr.Kind)
	}
	return err
}

func deleteStaleDevice(ctx context.Context, devices pushdevice.Repository, destination string, kind DeliveryErrorKind, attrs ...any) {
	// The destination is intentionally not passed to the logger. Callers must
	// provide it only to the repository so FIDs never enter structured logs.
	if err := devices.DeleteByDestination(ctx, destination); err != nil {
		// Keep the failure observable without emitting arbitrary repository error
		// text, which could contain a destination in an implementation-specific
		// error message.
		logAttrs := []any{"delivery_kind", kind, "cleanup_error_type", fmt.Sprintf("%T", err)}
		logAttrs = append(logAttrs, attrs...)
		slog.Default().Error("failed to delete stale push device", logAttrs...)
	}
}
