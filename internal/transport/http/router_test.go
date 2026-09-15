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

	expected := []string{"created_at", "destination", "id", "platform", "updated_at", "user_id"}
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
