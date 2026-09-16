package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	appnotification "github.com/sbezhuk/beebase-notification-service/internal/application/notification"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/pushdevice"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/reminder"
)

type deviceRepoFake struct {
	device *pushdevice.PushDevice
}

func (f *deviceRepoFake) Upsert(_ context.Context, d *pushdevice.PushDevice) error {
	f.device = d
	return nil
}

func (f *deviceRepoFake) Update(_ context.Context, d *pushdevice.PushDevice) error {
	if f.device == nil || f.device.ID != d.ID || f.device.UserID != d.UserID {
		return pushdevice.ErrNotFound
	}
	d.CreatedAt = f.device.CreatedAt
	f.device = d
	return nil
}

func (f *deviceRepoFake) FindByID(context.Context, uuid.UUID, uuid.UUID) (*pushdevice.PushDevice, error) {
	return f.device, nil
}

func (f *deviceRepoFake) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *deviceRepoFake) DeleteByDestination(context.Context, string) error {
	return nil
}
func (f *deviceRepoFake) DeleteAllByUser(context.Context, uuid.UUID) error            { return nil }
func (f *deviceRepoFake) DeleteBySession(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type senderFake struct{}

func (senderFake) Send(context.Context, appnotification.PushMessage) error {
	return nil
}

type parserFake struct {
	userID uuid.UUID
}

func (p parserFake) Parse(context.Context, string) (uuid.UUID, error) {
	return p.userID, nil
}

func TestRegisterDeviceResponseUsesOpenAPIFieldNames(t *testing.T) {
	router := newDeviceTestRouter(uuid.New())

	body := bytes.NewBufferString(`{"destination":"fid-post","platform":"ios"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/devices/", body)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	assertPushDeviceResponseKeys(t, rr.Body.Bytes())
}

func TestUpdateDeviceResponseUsesOpenAPIFieldNames(t *testing.T) {
	userID := uuid.New()
	router := newDeviceTestRouter(userID)

	registerBody := bytes.NewBufferString(`{"destination":"fid-original","platform":"ios"}`)
	registerReq := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/devices/", registerBody)
	registerReq.Header.Set("Authorization", "Bearer test-token")
	registerRR := httptest.NewRecorder()
	router.ServeHTTP(registerRR, registerReq)
	if registerRR.Code != stdhttp.StatusOK {
		t.Fatalf("register status = %d, body = %s", registerRR.Code, registerRR.Body.String())
	}
	var registered struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(registerRR.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}

	updateBody := bytes.NewBufferString(`{"destination":"fid-updated","platform":"ios"}`)
	updateReq := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/devices/"+registered.ID.String(), updateBody)
	updateReq.Header.Set("Authorization", "Bearer test-token")
	updateRR := httptest.NewRecorder()

	router.ServeHTTP(updateRR, updateReq)

	if updateRR.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", updateRR.Code, updateRR.Body.String())
	}
	assertPushDeviceResponseKeys(t, updateRR.Body.Bytes())
}

func newDeviceTestRouter(userID uuid.UUID) stdhttp.Handler {
	repo := &deviceRepoFake{
		device: &pushdevice.PushDevice{
			CreatedAt: time.Now().UTC(),
		},
	}
	svc := appnotification.NewService(repo, senderFake{})
	handler := NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, handler, parserFake{userID: userID}, "internal-test-token")
}

func assertPushDeviceResponseKeys(t *testing.T, body []byte) {
	t.Helper()
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}

	expected := []string{"createdAt", "destination", "id", "platform", "updatedAt", "userId"}
	actual := make([]string, 0, len(response))
	for key := range response {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	if len(actual) != len(expected) {
		t.Fatalf("response keys = %v, want exactly %v", actual, expected)
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Fatalf("response keys = %v, want exactly %v", actual, expected)
		}
	}

	for _, key := range []string{"ID", "UserID", "Destination", "Platform", "CreatedAt", "UpdatedAt"} {
		if _, ok := response[key]; ok {
			t.Fatalf("response contains PascalCase key %q: %s", key, string(body))
		}
	}
}

type reminderHTTPRepoFake struct {
	items map[uuid.UUID]*reminder.Reminder
}

func (f *reminderHTTPRepoFake) Create(_ context.Context, v *reminder.Reminder) error {
	if f.items == nil {
		f.items = map[uuid.UUID]*reminder.Reminder{}
	}
	f.items[v.ID] = v
	return nil
}
func (f *reminderHTTPRepoFake) Get(_ context.Context, id, user uuid.UUID) (*reminder.Reminder, error) {
	v := f.items[id]
	if v == nil || v.UserID != user {
		return nil, reminder.ErrNotFound
	}
	copy := *v
	return &copy, nil
}
func (f *reminderHTTPRepoFake) List(context.Context, uuid.UUID, reminder.Filter) ([]reminder.Reminder, int, error) {
	return nil, 0, nil
}
func (f *reminderHTTPRepoFake) Update(_ context.Context, v *reminder.Reminder) error {
	if f.items[v.ID] == nil {
		return reminder.ErrNotFound
	}
	f.items[v.ID] = v
	return nil
}
func (f *reminderHTTPRepoFake) Delete(context.Context, uuid.UUID, uuid.UUID) error  { return nil }
func (f *reminderHTTPRepoFake) Cleanup(context.Context, []reminder.EntityRef) error { return nil }
func (f *reminderHTTPRepoFake) DeleteAllByUser(context.Context, uuid.UUID) error    { return nil }
func (f *reminderHTTPRepoFake) ClaimDue(context.Context, time.Time, int, time.Duration) ([]reminder.Reminder, error) {
	return nil, nil
}
func (f *reminderHTTPRepoFake) MarkSent(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *reminderHTTPRepoFake) MarkCancelled(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (f *reminderHTTPRepoFake) MarkRetry(context.Context, uuid.UUID, uuid.UUID, reminder.Status, int, time.Time, string) error {
	return nil
}

type reminderEntityResolverFake struct{}

func (reminderEntityResolverFake) Exists(context.Context, reminder.EntityType, uuid.UUID) (bool, error) {
	return true, nil
}

func newReminderTestRouter(userID uuid.UUID, repo *reminderHTTPRepoFake) stdhttp.Handler {
	service := appnotification.NewReminderService(repo, &deviceRepoFake{}, senderFake{}, reminderEntityResolverFake{})
	handler := NewHandler(appnotification.NewService(&deviceRepoFake{}, senderFake{}), slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	return NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, handler, parserFake{userID: userID}, "internal-test-token")
}

func TestReminderHTTPParsesOffsetsAndSerializesResponsesInUTC(t *testing.T) {
	userID := uuid.New()
	repo := &reminderHTTPRepoFake{}
	router := newReminderTestRouter(userID, repo)

	create := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/reminders/", bytes.NewBufferString(`{"title":"Inspect","entityType":"hive","entityId":"33333333-3333-4333-8333-333333333333","remindAt":"2030-01-02T08:30:00+05:30"}`))
	create.Header.Set("Authorization", "Bearer test-token")
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, create)
	if createRR.Code != stdhttp.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRR.Code, createRR.Body.String())
	}
	var created struct {
		ID       uuid.UUID `json:"id"`
		RemindAt string    `json:"remindAt"`
	}
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.RemindAt != "2030-01-02T03:00:00Z" {
		t.Fatalf("create remindAt = %q, want UTC", created.RemindAt)
	}

	update := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/reminders/"+created.ID.String(), bytes.NewBufferString(`{"title":"Updated","entityType":"hive","entityId":"33333333-3333-4333-8333-333333333333","remindAt":"2030-01-02T03:00:00Z"}`))
	update.Header.Set("Authorization", "Bearer test-token")
	updateRR := httptest.NewRecorder()
	router.ServeHTTP(updateRR, update)
	if updateRR.Code != stdhttp.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateRR.Code, updateRR.Body.String())
	}
	var updated struct {
		RemindAt string `json:"remindAt"`
	}
	if err := json.Unmarshal(updateRR.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.RemindAt != created.RemindAt {
		t.Fatalf("equivalent update instant changed: create=%q update=%q", created.RemindAt, updated.RemindAt)
	}
}

func TestReminderHTTPRejectsTimezoneLessAndPastRemindAt(t *testing.T) {
	userID := uuid.New()
	router := newReminderTestRouter(userID, &reminderHTTPRepoFake{})
	for _, tc := range []struct {
		name string
		at   string
	}{
		{name: "timezone-less", at: "2030-01-02T03:00:00"},
		{name: "past", at: "2000-01-02T03:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/reminders/", bytes.NewBufferString(`{"title":"Inspect","entityType":"hive","entityId":"33333333-3333-4333-8333-333333333333","remindAt":"`+tc.at+`"}`))
			req.Header.Set("Authorization", "Bearer test-token")
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != stdhttp.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
			}
		})
	}
}
