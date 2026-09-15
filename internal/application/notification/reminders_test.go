package notification

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/reminder"
)

type reminderRepoFake struct {
	mu          sync.Mutex
	items       map[uuid.UUID]*reminder.Reminder
	filter      reminder.Filter
	claim       []reminder.Reminder
	claimCalls  int
	marked      string
	markedToken uuid.UUID
	next        time.Time
	attempt     int
	lease       time.Duration
}

func (f *reminderRepoFake) Create(_ context.Context, v *reminder.Reminder) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.items == nil {
		f.items = map[uuid.UUID]*reminder.Reminder{}
	}
	f.items[v.ID] = v
	return nil
}
func (f *reminderRepoFake) Get(_ context.Context, id, user uuid.UUID) (*reminder.Reminder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.items[id]
	if v == nil || v.UserID != user {
		return nil, reminder.ErrNotFound
	}
	c := *v
	return &c, nil
}
func (f *reminderRepoFake) List(_ context.Context, _ uuid.UUID, filter reminder.Filter) ([]reminder.Reminder, int, error) {
	f.filter = filter
	return f.claim, len(f.claim), nil
}
func (f *reminderRepoFake) Update(_ context.Context, v *reminder.Reminder) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.items[v.ID] == nil {
		return reminder.ErrNotFound
	}
	f.items[v.ID] = v
	return nil
}
func (f *reminderRepoFake) Delete(_ context.Context, id, user uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.items[id]
	if v == nil || v.UserID != user {
		return reminder.ErrNotFound
	}
	delete(f.items, id)
	return nil
}
func (f *reminderRepoFake) Cleanup(_ context.Context, es []reminder.EntityRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range es {
		for _, v := range f.items {
			if v.EntityType == e.Type && v.EntityID == e.ID && v.Status != reminder.StatusSent && v.Status != reminder.StatusCancelled {
				v.Status = reminder.StatusCancelled
				r := "entity_deleted"
				v.CancelReason = &r
			}
		}
	}
	return nil
}
func (f *reminderRepoFake) ClaimDue(_ context.Context, _ time.Time, _ int, lease time.Duration) ([]reminder.Reminder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lease = lease
	f.claimCalls++
	if f.claimCalls > 1 {
		return nil, nil
	}
	out := make([]reminder.Reminder, len(f.claim))
	copy(out, f.claim)
	for i := range out {
		out[i].Status = reminder.StatusProcessing
		out[i].ProcessingToken = uuid.New()
		until := time.Now().Add(lease)
		out[i].ProcessingLeaseUntil = &until
	}
	return out, nil
}
func (f *reminderRepoFake) MarkSent(_ context.Context, id, token uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marked = "sent"
	f.markedToken = token
	if v := f.items[id]; v != nil {
		v.Status = reminder.StatusSent
	}
	return nil
}
func (f *reminderRepoFake) MarkCancelled(_ context.Context, id, token uuid.UUID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marked = "cancelled"
	f.markedToken = token
	if v := f.items[id]; v != nil {
		v.Status = reminder.StatusCancelled
	}
	return nil
}
func (f *reminderRepoFake) MarkRetry(_ context.Context, id, token uuid.UUID, status reminder.Status, attempt int, next time.Time, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marked = string(status)
	f.markedToken = token
	f.next = next
	f.attempt = attempt
	if v := f.items[id]; v != nil {
		v.Status = status
		v.AttemptCount = attempt
	}
	return nil
}

type reminderDevicesFake struct {
	fakeRepo
	devices []pushdevice.PushDevice
	removed []string
}

func (f *reminderDevicesFake) ListByUser(context.Context, uuid.UUID) ([]pushdevice.PushDevice, error) {
	return f.devices, nil
}
func (f *reminderDevicesFake) DeleteByDestination(_ context.Context, d string) error {
	f.removed = append(f.removed, d)
	return nil
}

type resolverFake struct {
	exists bool
	err    error
}

func (f resolverFake) Exists(context.Context, reminder.EntityType, uuid.UUID) (bool, error) {
	return f.exists, f.err
}

type senderFake struct {
	mu    sync.Mutex
	calls []PushMessage
	err   error
	errs  []error
}

func (f *senderFake) Send(_ context.Context, m PushMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, m)
	if len(f.errs) >= len(f.calls) {
		return f.errs[len(f.calls)-1]
	}
	return f.err
}

func newReminder(user uuid.UUID) reminder.Reminder {
	return reminder.Reminder{ID: uuid.New(), UserID: user, Title: "Check hive", Note: "Inspect queen", EntityType: reminder.EntityHive, EntityID: uuid.New(), RemindAt: time.Now().Add(-time.Minute), NextAttemptAt: time.Now().Add(-time.Minute), Status: reminder.StatusScheduled}
}

func TestReminderCRUDOwnershipAndFiltering(t *testing.T) {
	user, other := uuid.New(), uuid.New()
	repo := &reminderRepoFake{}
	svc := NewReminderService(repo, &reminderDevicesFake{}, &senderFake{}, resolverFake{exists: true})
	v, err := svc.Create(context.Background(), user, CreateReminderInput{Title: " title ", EntityType: reminder.EntityHive, EntityID: uuid.New(), RemindAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if v.UserID != user || v.Source != "manual" || v.ReminderType != "custom" {
		t.Fatalf("bad defaults: %#v", v)
	}
	if _, err := svc.Get(context.Background(), other, v.ID); !errors.Is(err, reminder.ErrNotFound) {
		t.Fatalf("other user accessed reminder: %v", err)
	}
	typ := reminder.EntityHive
	id := v.EntityID
	status := reminder.StatusScheduled
	_, _, _ = svc.List(context.Background(), user, reminder.Filter{EntityType: &typ, EntityID: &id, Status: &status, Page: 2, Limit: 10})
	if repo.filter.Page != 2 || repo.filter.Limit != 10 || repo.filter.EntityType == nil || repo.filter.EntityID == nil || repo.filter.Status == nil {
		t.Fatalf("filters not forwarded: %#v", repo.filter)
	}
	if err := svc.Delete(context.Background(), other, v.ID); !errors.Is(err, reminder.ErrNotFound) {
		t.Fatalf("other user deleted reminder: %v", err)
	}
	if err := svc.Delete(context.Background(), user, v.ID); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDueEntityExistsSendsAndIncludesNavigationData(t *testing.T) {
	user := uuid.New()
	repo := &reminderRepoFake{}
	v := newReminder(user)
	repo.claim = []reminder.Reminder{v}
	sender := &senderFake{}
	svc := NewReminderService(repo, &reminderDevicesFake{devices: []pushdevice.PushDevice{{UserID: user, Destination: "good"}}}, sender, resolverFake{exists: true})
	if err := svc.ProcessDue(context.Background(), time.Now(), 10); err != nil {
		t.Fatal(err)
	}
	if repo.marked != "sent" || len(sender.calls) != 1 {
		t.Fatalf("expected sent once: marked=%s calls=%d", repo.marked, len(sender.calls))
	}
	if sender.calls[0].Data["type"] != "reminder" || sender.calls[0].Data["entity_id"] != v.EntityID.String() {
		t.Fatalf("missing navigation payload: %#v", sender.calls[0].Data)
	}
}
func TestProcessDueDeletedEntityCancelsWithoutSending(t *testing.T) {
	repo := &reminderRepoFake{}
	v := newReminder(uuid.New())
	repo.claim = []reminder.Reminder{v}
	sender := &senderFake{}
	svc := NewReminderService(repo, &reminderDevicesFake{}, sender, resolverFake{exists: false})
	_ = svc.ProcessDue(context.Background(), time.Now(), 10)
	if repo.marked != "cancelled" || len(sender.calls) != 0 {
		t.Fatalf("deleted entity was delivered: marked=%s calls=%d", repo.marked, len(sender.calls))
	}
}
func TestProcessDueResolverFailureRetriesWithoutSending(t *testing.T) {
	repo := &reminderRepoFake{}
	v := newReminder(uuid.New())
	repo.claim = []reminder.Reminder{v}
	sender := &senderFake{}
	svc := NewReminderService(repo, &reminderDevicesFake{}, sender, resolverFake{err: errors.New("timeout")})
	now := time.Now()
	_ = svc.ProcessDue(context.Background(), now, 10)
	if repo.marked != "scheduled" || len(sender.calls) != 0 || repo.attempt != 1 || repo.next.Before(now.Add(2*time.Minute)) {
		t.Fatalf("resolver failure not retried safely: marked=%s calls=%d attempt=%d next=%v", repo.marked, len(sender.calls), repo.attempt, repo.next)
	}
}
func TestProcessDueTemporaryFirebaseRetriesPermanentFails(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *DeliveryError
		want string
	}{{"temporary", &DeliveryError{Kind: DeliveryTemporary, Err: errors.New("unavailable")}, "scheduled"}, {"auth", &DeliveryError{Kind: DeliveryAuthentication, Err: errors.New("bad credentials")}, "failed"}, {"unknown", &DeliveryError{Kind: DeliveryUnknown, Err: errors.New("rejected")}, "failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &reminderRepoFake{}
			v := newReminder(uuid.New())
			repo.claim = []reminder.Reminder{v}
			svc := NewReminderService(repo, &reminderDevicesFake{devices: []pushdevice.PushDevice{{UserID: v.UserID, Destination: "d"}}}, &senderFake{err: tc.err}, resolverFake{exists: true})
			_ = svc.ProcessDue(context.Background(), time.Now(), 10)
			if repo.marked != tc.want {
				t.Fatalf("got %s want %s", repo.marked, tc.want)
			}
		})
	}
}
func TestProcessDueMultipleDevicesStaleDeviceDoesNotFailDelivery(t *testing.T) {
	repo := &reminderRepoFake{}
	v := newReminder(uuid.New())
	repo.claim = []reminder.Reminder{v}
	stale := &DeliveryError{Kind: DeliveryUnregistered, Err: errors.New("stale")}
	sender := &senderFake{errs: []error{stale, nil}}
	devices := &reminderDevicesFake{devices: []pushdevice.PushDevice{{UserID: v.UserID, Destination: "stale"}, {UserID: v.UserID, Destination: "good"}}}
	svc := NewReminderService(repo, devices, sender, resolverFake{exists: true})
	_ = svc.ProcessDue(context.Background(), time.Now(), 10)
	if repo.marked != "sent" || len(devices.removed) != 1 || len(sender.calls) != 2 {
		t.Fatalf("multi-device semantics failed: marked=%s removed=%v calls=%d", repo.marked, devices.removed, len(sender.calls))
	}
}
func TestProcessDueConcurrentWorkersClaimOnce(t *testing.T) {
	repo := &reminderRepoFake{}
	v := newReminder(uuid.New())
	repo.claim = []reminder.Reminder{v}
	sender := &senderFake{}
	svc := NewReminderService(repo, &reminderDevicesFake{devices: []pushdevice.PushDevice{{UserID: v.UserID, Destination: "good"}}}, sender, resolverFake{exists: true})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = svc.ProcessDue(context.Background(), time.Now(), 10) }()
	}
	wg.Wait()
	if len(sender.calls) != 1 {
		t.Fatalf("claimed reminder delivered %d times", len(sender.calls))
	}
}
func TestProcessDueUsesFiniteProcessingLease(t *testing.T) {
	repo := &reminderRepoFake{}
	v := newReminder(uuid.New())
	repo.claim = []reminder.Reminder{v}
	svc := NewReminderService(repo, &reminderDevicesFake{devices: []pushdevice.PushDevice{{UserID: v.UserID, Destination: "good"}}}, &senderFake{}, resolverFake{exists: true})
	_ = svc.ProcessDue(context.Background(), time.Now(), 10)
	if repo.lease <= 0 {
		t.Fatal("claim did not establish a recovery lease")
	}
}
func TestCleanupIsIdempotent(t *testing.T) {
	repo := &reminderRepoFake{}
	v := newReminder(uuid.New())
	repo.items = map[uuid.UUID]*reminder.Reminder{v.ID: &v}
	svc := NewReminderService(repo, &reminderDevicesFake{}, &senderFake{}, resolverFake{})
	ref := []reminder.EntityRef{{Type: v.EntityType, ID: v.EntityID}}
	if err := svc.Cleanup(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := svc.Cleanup(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if repo.items[v.ID].Status != reminder.StatusCancelled {
		t.Fatalf("cleanup not idempotent")
	}
}
