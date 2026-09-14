package notification

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
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
	if strings.TrimSpace(destination) == "" {
		return nil, fmt.Errorf("destination is required")
	}
	if platform != pushdevice.PlatformIOS && platform != pushdevice.PlatformAndroid {
		return nil, fmt.Errorf("platform must be ios or android")
	}
	now := time.Now().UTC()
	d := &pushdevice.PushDevice{ID: uuid.New(), UserID: userID, Destination: destination, Platform: platform, CreatedAt: now, UpdatedAt: now}
	if err := s.devices.Upsert(ctx, d); err != nil {
		return nil, fmt.Errorf("register device: %w", err)
	}
	return d, nil
}
func (s *Service) RemoveDevice(ctx context.Context, id, userID uuid.UUID) error {
	if err := s.devices.Delete(ctx, id, userID); err != nil {
		return fmt.Errorf("remove device: %w", err)
	}
	return nil
}
func (s *Service) Send(ctx context.Context, m PushMessage) error {
	if strings.TrimSpace(m.Destination) == "" {
		return fmt.Errorf("destination is required")
	}
	if strings.TrimSpace(m.Title) == "" || strings.TrimSpace(m.Body) == "" {
		return fmt.Errorf("title and body are required")
	}
	return s.sender.Send(ctx, m)
}
