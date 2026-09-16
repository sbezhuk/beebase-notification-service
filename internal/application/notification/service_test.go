package notification

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
	"sync"
	"testing"
)

type oneRowRepo struct {
	mu      sync.Mutex
	devices map[uuid.UUID]pushdevice.PushDevice
}

func (r *oneRowRepo) Upsert(_ context.Context, d *pushdevice.PushDevice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.devices == nil {
		r.devices = map[uuid.UUID]pushdevice.PushDevice{}
	}
	if current, ok := r.devices[d.UserID]; ok && d.SessionGeneration < current.SessionGeneration {
		return pushdevice.ErrStaleSession
	}
	r.devices[d.UserID] = *d
	return nil
}
func (r *oneRowRepo) Update(_ context.Context, d *pushdevice.PushDevice) error {
	return r.Upsert(context.Background(), d)
}
func (r *oneRowRepo) FindByID(_ context.Context, id, user uuid.UUID) (*pushdevice.PushDevice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[user]
	if !ok || d.ID != id {
		return nil, pushdevice.ErrNotFound
	}
	return &d, nil
}
func (r *oneRowRepo) Delete(_ context.Context, id, user uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[user]
	if !ok || d.ID != id {
		return pushdevice.ErrNotFound
	}
	delete(r.devices, user)
	return nil
}
func (r *oneRowRepo) DeleteByDestination(context.Context, string) error { return nil }
func (r *oneRowRepo) DeleteAllByUser(_ context.Context, user uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.devices, user)
	return nil
}
func (r *oneRowRepo) DeleteBySession(_ context.Context, user, session uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.devices[user]; ok && d.SessionID == session {
		delete(r.devices, user)
	}
	return nil
}

func TestConcurrentRegistrationsLeaveOneAuthoritativeRow(t *testing.T) {
	repo := &oneRowRepo{}
	user, session := uuid.New(), uuid.New()
	svc := NewService(repo, &fakeSender{}, activeSessionFake{active: session})
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, destination := range []string{"fid-a", "fid-b"} {
		wg.Add(1)
		go func(destination string) {
			defer wg.Done()
			<-start
			if _, err := svc.RegisterDeviceForSession(context.Background(), user, destination, pushdevice.PlatformAndroid, session, 4); err != nil {
				t.Errorf("register %s: %v", destination, err)
			}
		}(destination)
	}
	close(start)
	wg.Wait()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.devices) != 1 {
		t.Fatalf("authoritative rows = %d, want 1", len(repo.devices))
	}
}

func TestOlderGenerationCannotOverwriteCurrentRegistration(t *testing.T) {
	repo := &oneRowRepo{}
	user, session := uuid.New(), uuid.New()
	svc := NewService(repo, &fakeSender{}, activeSessionFake{active: session})
	if _, err := svc.RegisterDeviceForSession(context.Background(), user, "fid-current", pushdevice.PlatformAndroid, session, 8); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterDeviceForSession(context.Background(), user, "fid-old", pushdevice.PlatformAndroid, session, 7); !errors.Is(err, pushdevice.ErrStaleSession) {
		t.Fatalf("error = %v, want stale session", err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if got := repo.devices[user].Destination; got != "fid-current" {
		t.Fatalf("destination = %q, want fid-current", got)
	}
}

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
func (f *fakeRepo) DeleteAllByUser(context.Context, uuid.UUID) error            { return nil }
func (f *fakeRepo) DeleteBySession(context.Context, uuid.UUID, uuid.UUID) error { return nil }
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

func TestRegisterDeviceRejectsSupersededSession(t *testing.T) {
	repo := &fakeRepo{}
	user, active, old := uuid.New(), uuid.New(), uuid.New()
	svc := NewService(repo, &fakeSender{}, activeSessionFake{active: active})
	if _, err := svc.RegisterDevice(context.Background(), user, "fid-old", pushdevice.PlatformAndroid, old); !errors.Is(err, pushdevice.ErrInactiveSession) {
		t.Fatalf("error = %v, want inactive session", err)
	}
	if _, err := svc.RegisterDevice(context.Background(), user, "fid-current", pushdevice.PlatformAndroid, active); err != nil {
		t.Fatalf("current session registration: %v", err)
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
