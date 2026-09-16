package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
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
		for id, v := range f.items {
			if v.EntityType == e.Type && v.EntityID == e.ID {
				delete(f.items, id)
			}
		}
	}
	return nil
}
func (f *reminderRepoFake) DeleteAllByUser(context.Context, uuid.UUID) error { return nil }
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
	devices   []pushdevice.PushDevice
	removed   []string
	deleteErr error
}

func (f *reminderDevicesFake) ListByUser(context.Context, uuid.UUID) ([]pushdevice.PushDevice, error) {
	return f.devices, nil
}
func (f *reminderDevicesFake) DeleteByDestination(_ context.Context, d string) error {
	f.removed = append(f.removed, d)
	return f.deleteErr
}
func (f *reminderDevicesFake) DeleteAllByUser(context.Context, uuid.UUID) error { return nil }
func (f *reminderDevicesFake) DeleteBySession(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

type resolverFake struct {
	exists bool
	err    error
}

type activeSessionFake struct{ active uuid.UUID }

func (f activeSessionFake) IsActive(_ context.Context, _ uuid.UUID, session uuid.UUID) (bool, error) {
	return session == f.active, nil
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

func TestCreateNormalizesEquivalentOffsetInstantsToUTC(t *testing.T) {
	user := uuid.New()
	base := time.Date(2030, time.March, 31, 1, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		at   time.Time
	}{
		{name: "positive offset at DST boundary", at: time.Date(2030, time.March, 31, 3, 30, 0, 0, time.FixedZone("UTC+2", 2*60*60))},
		{name: "negative offset", at: time.Date(2030, time.March, 30, 20, 30, 0, 0, time.FixedZone("UTC-5", -5*60*60))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !base.Equal(tc.at) {
				t.Fatal("test timestamps must represent the same instant")
			}
			repo := &reminderRepoFake{}
			service := NewReminderService(repo, &reminderDevicesFake{}, &senderFake{}, resolverFake{exists: true})
			got, err := service.Create(context.Background(), user, CreateReminderInput{Title: tc.name, EntityType: reminder.EntityHive, EntityID: uuid.New(), RemindAt: tc.at})
			if err != nil {
				t.Fatal(err)
			}
			if !got.RemindAt.Equal(base) || got.RemindAt.Location() != time.UTC {
				t.Fatalf("equivalent instant was not canonicalized: %v", got.RemindAt)
			}
		})
	}
}

func TestCreateAndUpdateRejectPastInstantsRegardlessOfOffset(t *testing.T) {
	user := uuid.New()
	past := time.Now().UTC().Add(-time.Minute)
	pastWithOffset := past.In(time.FixedZone("UTC-7", -7*60*60))
	repo := &reminderRepoFake{}
	service := NewReminderService(repo, &reminderDevicesFake{}, &senderFake{}, resolverFake{exists: true})
	input := CreateReminderInput{Title: "past", EntityType: reminder.EntityHive, EntityID: uuid.New(), RemindAt: pastWithOffset}
	if _, err := service.Create(context.Background(), user, input); err == nil {
		t.Fatal("create accepted a past instant")
	}

	future, err := service.Create(context.Background(), user, CreateReminderInput{Title: "future", EntityType: reminder.EntityHive, EntityID: uuid.New(), RemindAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	input.RemindAt = past
	if _, err := service.Update(context.Background(), user, future.ID, input); err == nil {
		t.Fatal("update accepted a past instant")
	}
}

func TestReminderJSONUsesUTCForEveryExposedTimestamp(t *testing.T) {
	zone := time.FixedZone("UTC+5:30", 5*60*60+30*60)
	value := time.Date(2030, time.January, 2, 8, 30, 0, 0, zone)
	reminderValue := reminder.Reminder{RemindAt: value, NextAttemptAt: value, CreatedAt: value, UpdatedAt: value}
	body, err := json.Marshal(reminderValue)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"remindAt", "nextAttemptAt", "createdAt", "updatedAt"} {
		var value string
		if err := json.Unmarshal(got[field], &value); err != nil {
			t.Fatal(err)
		}
		if value != "2030-01-02T03:00:00Z" {
			t.Fatalf("%s = %q, want UTC timestamp", field, value)
		}
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

func TestProcessDueSendsOnlyToTheCurrentSessionDevice(t *testing.T) {
	user := uuid.New()
	oldSession, currentSession := uuid.New(), uuid.New()
	repo := &reminderRepoFake{}
	v := newReminder(user)
	repo.claim = []reminder.Reminder{v}
	sender := &senderFake{}
	devices := &reminderDevicesFake{devices: []pushdevice.PushDevice{
		{UserID: user, SessionID: oldSession, SessionGeneration: 1, Destination: "fid-a"},
		{UserID: user, SessionID: currentSession, SessionGeneration: 2, Destination: "fid-b"},
	}}
	svc := NewReminderService(repo, devices, sender, resolverFake{exists: true}, activeSessionFake{active: currentSession})
	if err := svc.ProcessDue(context.Background(), time.Now(), 10); err != nil {
		t.Fatal(err)
	}
	if len(sender.calls) != 1 || sender.calls[0].Destination != "fid-b" {
		t.Fatalf("push destinations = %#v, want only fid-b", sender.calls)
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

func TestProcessDueContinuesWhenStaleDeviceCleanupFails(t *testing.T) {
	repo := &reminderRepoFake{}
	v := newReminder(uuid.New())
	repo.claim = []reminder.Reminder{v}
	devices := &reminderDevicesFake{
		devices:   []pushdevice.PushDevice{{ID: uuid.New(), UserID: v.UserID, Destination: "stale"}, {ID: uuid.New(), UserID: v.UserID, Destination: "good"}},
		deleteErr: errors.New("database unavailable"),
	}
	old := slog.Default()
	defer slog.SetDefault(old)
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	svc := NewReminderService(repo, devices, &senderFake{errs: []error{
		&DeliveryError{Kind: DeliveryUnregistered, Err: errors.New("stale")}, nil,
	}}, resolverFake{exists: true})

	if err := svc.ProcessDue(context.Background(), time.Now(), 10); err != nil {
		t.Fatal(err)
	}
	if repo.marked != "sent" || len(devices.removed) != 1 {
		t.Fatalf("cleanup failure changed delivery semantics: marked=%s removed=%v", repo.marked, devices.removed)
	}
	if !strings.Contains(logs.String(), "failed to delete stale push device") || !strings.Contains(logs.String(), "unregistered_destination") {
		t.Fatalf("cleanup failure was not logged safely: %q", logs.String())
	}
	if strings.Contains(logs.String(), "destination=") || strings.Contains(logs.String(), "fid-") {
		t.Fatalf("log contains a destination: %q", logs.String())
	}
}

func TestProcessDueAttemptsDevicesAfterFirstSuccess(t *testing.T) {
	repo := &reminderRepoFake{}
	v := newReminder(uuid.New())
	repo.claim = []reminder.Reminder{v}
	devices := &reminderDevicesFake{devices: []pushdevice.PushDevice{{UserID: v.UserID, Destination: "good"}, {UserID: v.UserID, Destination: "stale"}}}
	sender := &senderFake{errs: []error{nil, &DeliveryError{Kind: DeliveryUnregistered, Err: errors.New("stale")}}}
	svc := NewReminderService(repo, devices, sender, resolverFake{exists: true})
	_ = svc.ProcessDue(context.Background(), time.Now(), 10)
	if repo.marked != "sent" || len(sender.calls) != 2 || len(devices.removed) != 1 {
		t.Fatalf("first success short-circuited delivery: marked=%s calls=%d removed=%v", repo.marked, len(sender.calls), devices.removed)
	}
}

func TestProcessDueAllUnregisteredDevicesRetainsPermanentFailure(t *testing.T) {
	repo := &reminderRepoFake{}
	v := newReminder(uuid.New())
	repo.claim = []reminder.Reminder{v}
	devices := &reminderDevicesFake{devices: []pushdevice.PushDevice{
		{UserID: v.UserID, Destination: "stale-1"},
		{UserID: v.UserID, Destination: "stale-2"},
	}}
	sender := &senderFake{errs: []error{
		&DeliveryError{Kind: DeliveryUnregistered, Err: errors.New("stale")},
		&DeliveryError{Kind: DeliveryUnregistered, Err: errors.New("stale")},
	}}
	svc := NewReminderService(repo, devices, sender, resolverFake{exists: true})
	_ = svc.ProcessDue(context.Background(), time.Now(), 10)
	if repo.marked != "failed" || len(sender.calls) != 2 || len(devices.removed) != 2 {
		t.Fatalf("all stale devices were not processed correctly: marked=%s calls=%d removed=%v", repo.marked, len(sender.calls), devices.removed)
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
	if _, ok := repo.items[v.ID]; ok {
		t.Fatalf("cleanup did not hard-delete reminder")
	}
}
