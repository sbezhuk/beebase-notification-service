package entityclient

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/reminder"
)

func TestClientExistsDistinguishesMissingAndUnavailable(t *testing.T) {
	id := uuid.New()
	client := New(map[reminder.EntityType]string{reminder.EntityApiary: "http://apiary"})
	client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/internal/api/v1/apiaries/"+id.String()+"/exists" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(nil), Header: make(http.Header)}, nil
	})}
	exists, err := client.Exists(context.Background(), reminder.EntityApiary, id)
	if err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	client = New(map[reminder.EntityType]string{reminder.EntityHive: "http://hive"})
	client.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(nil), Header: make(http.Header)}, nil
	})}
	exists, err = client.Exists(context.Background(), reminder.EntityHive, id)
	if err != nil || exists {
		t.Fatalf("missing=%v err=%v", exists, err)
	}
	client = New(map[reminder.EntityType]string{reminder.EntityHive: "http://127.0.0.1:1"})
	if exists, err = client.Exists(context.Background(), reminder.EntityHive, id); err == nil || exists {
		t.Fatalf("unavailable=%v err=%v", exists, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
