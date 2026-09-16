package notification

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
	"testing"
)

type fakeRepo struct {
	device             *pushdevice.PushDevice
	deleted            uuid.UUID
	deletedDestination string
	deleteErr          error
	upsertErr          error
	updateErr          error
}

func (f *fakeRepo) Upsert(_ context.Context, d *pushdevice.PushDevice) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.device = d
	return nil
}
func (f *fakeRepo) Update(_ context.Context, d *pushdevice.PushDevice) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	if f.device == nil || f.device.ID != d.ID || f.device.UserID != d.UserID {
		return pushdevice.ErrNotFound
	}
	f.device = d
	return nil
}
func (f *fakeRepo) FindByID(context.Context, uuid.UUID, uuid.UUID) (*pushdevice.PushDevice, error) {
	return f.device, nil
}
func (f *fakeRepo) DeleteByDestination(_ context.Context, destination string) error {
	f.deletedDestination = destination
	return f.deleteErr
}
func (f *fakeRepo) DeleteAllByUser(context.Context, uuid.UUID) error { return nil }
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

type failingSender struct{ err error }

func (f failingSender) Send(context.Context, PushMessage) error { return f.err }

func TestSendRemovesDefinitivelyInvalidDestination(t *testing.T) {
	repo := &fakeRepo{}
	providerErr := &DeliveryError{Kind: DeliveryUnregistered, Err: errors.New("provider rejected destination")}
	svc := NewService(repo, failingSender{err: providerErr})
	if err := svc.Send(context.Background(), PushMessage{Destination: "fid-stale", Title: "title", Body: "body"}); err == nil {
		t.Fatal("expected provider error")
	}
	if repo.deletedDestination != "fid-stale" {
		t.Fatalf("deleted destination = %q", repo.deletedDestination)
	}
}

func TestSendDoesNotRemoveOnTemporaryFailure(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, failingSender{err: &DeliveryError{Kind: DeliveryTemporary, Err: errors.New("temporary")}})
	_ = svc.Send(context.Background(), PushMessage{Destination: "fid-live", Title: "title", Body: "body"})
	if repo.deletedDestination != "" {
		t.Fatalf("temporary failure deleted %q", repo.deletedDestination)
	}
}

func TestSendDoesNotRemoveOnGenericInvalidArgument(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, failingSender{err: &DeliveryError{Kind: DeliveryUnknown, Err: errors.New("invalid request")}})
	_ = svc.Send(context.Background(), PushMessage{Destination: "fid-live", Title: "title", Body: "body"})
	if repo.deletedDestination != "" {
		t.Fatalf("generic invalid argument deleted %q", repo.deletedDestination)
	}
}

func TestSendDoesNotRemoveOnAuthenticationFailure(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, failingSender{err: &DeliveryError{Kind: DeliveryAuthentication, Err: errors.New("sender mismatch")}})
	_ = svc.Send(context.Background(), PushMessage{Destination: "fid-live", Title: "title", Body: "body"})
	if repo.deletedDestination != "" {
		t.Fatalf("authentication failure deleted %q", repo.deletedDestination)
	}
}

func TestSendKeepsDeliveryErrorWhenStaleCleanupFails(t *testing.T) {
	repo := &fakeRepo{deleteErr: errors.New("database unavailable")}
	providerErr := &DeliveryError{Kind: DeliveryUnregistered, Err: errors.New("provider rejected destination")}
	svc := NewService(repo, failingSender{err: providerErr})
	if err := svc.Send(context.Background(), PushMessage{Destination: "fid-stale", Title: "title", Body: "body"}); !errors.Is(err, providerErr) {
		t.Fatalf("got %v, want original delivery error", err)
	}
}

func TestRegisterRejectsDestinationOwnedByAnotherUser(t *testing.T) {
	repo := &fakeRepo{upsertErr: pushdevice.ErrDestinationOwned}
	svc := NewService(repo, &fakeSender{})
	if _, err := svc.RegisterDevice(context.Background(), uuid.New(), "fid-owned", pushdevice.PlatformIOS); !errors.Is(err, pushdevice.ErrDestinationOwned) {
		t.Fatalf("error = %v, want destination ownership error", err)
	}
}

func TestUpdatePreservesDeviceIdentityDuringDestinationRotation(t *testing.T) {
	uid, id := uuid.New(), uuid.New()
	repo := &fakeRepo{device: &pushdevice.PushDevice{ID: id, UserID: uid}}
	svc := NewService(repo, &fakeSender{})
	d, err := svc.UpdateDevice(context.Background(), uid, id, "fid-rotated", pushdevice.PlatformIOS)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != id || d.Destination != "fid-rotated" {
		t.Fatalf("updated device = %#v", d)
	}
}
