package notification

import (
	"context"
	"github.com/google/uuid"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
	"testing"
)

type fakeRepo struct {
	device  *pushdevice.PushDevice
	deleted uuid.UUID
}

func (f *fakeRepo) Upsert(_ context.Context, d *pushdevice.PushDevice) error {
	f.device = d
	return nil
}
func (f *fakeRepo) FindByID(context.Context, uuid.UUID, uuid.UUID) (*pushdevice.PushDevice, error) {
	return f.device, nil
}
func (f *fakeRepo) Delete(_ context.Context, id, user uuid.UUID) error {
	f.deleted = id
	if f.device == nil || f.device.UserID != user {
		return pushdevice.ErrNotFound
	}
	return nil
}

type fakeSender struct{ message PushMessage }

func (f *fakeSender) Send(_ context.Context, m PushMessage) error { f.message = m; return nil }

func TestRegisterDeviceIsSafeToRepeat(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, &fakeSender{})
	uid := uuid.New()
	first, err := svc.RegisterDevice(context.Background(), uid, "fid-1", pushdevice.PlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.RegisterDevice(context.Background(), uid, "fid-1", pushdevice.PlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	if first.Destination != second.Destination || repo.device.Destination != "fid-1" {
		t.Fatalf("duplicate registration was not idempotent: %#v %#v", first, second)
	}
}
func TestRemoveDeviceUsesAuthenticatedOwner(t *testing.T) {
	repo := &fakeRepo{device: &pushdevice.PushDevice{UserID: uuid.New()}}
	svc := NewService(repo, &fakeSender{})
	if err := svc.RemoveDevice(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("expected owner check")
	}
}
func TestSendDelegatesGenericPayload(t *testing.T) {
	sender := &fakeSender{}
	svc := NewService(&fakeRepo{}, sender)
	m := PushMessage{Destination: "fid-1", Title: "BeeBase test", Body: "FCM works", Data: map[string]string{"type": "test"}}
	if err := svc.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if sender.message.Destination != "fid-1" || sender.message.Data["type"] != "test" {
		t.Fatalf("message not delegated: %#v", sender.message)
	}
}
