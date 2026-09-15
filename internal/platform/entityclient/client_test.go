package entityclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/reminder"
)

func TestExistsAuthenticationAndStatusSemantics(t *testing.T) {
	const token = "internal-test-token"
	id := uuid.New()
	cases := []struct {
		name       string
		status     int
		wantExists bool
		wantErr    bool
	}{
		{name: "exists", status: http.StatusNoContent, wantExists: true},
		{name: "not found", status: http.StatusNotFound},
		{name: "unauthorized", status: http.StatusUnauthorized, wantErr: true},
		{name: "forbidden", status: http.StatusForbidden, wantErr: true},
		{name: "server error", status: http.StatusBadGateway, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotAuth string
			client := New(map[reminder.EntityType]string{reminder.EntityApiary: "http://entity-service"}, token)
			client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				gotAuth = r.Header.Get("Authorization")
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
			})
			got, err := client.Exists(context.Background(), reminder.EntityApiary, id)
			if got != tc.wantExists || (err != nil) != tc.wantErr {
				t.Fatalf("Exists() = (%v, %v), want (%v, error=%v)", got, err, tc.wantExists, tc.wantErr)
			}
			if gotAuth != "Bearer "+token {
				t.Fatalf("Authorization = %q, want internal bearer token", gotAuth)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExistsNetworkFailureIsError(t *testing.T) {
	client := New(map[reminder.EntityType]string{reminder.EntityApiary: "http://127.0.0.1:1"}, "token")
	got, err := client.Exists(context.Background(), reminder.EntityApiary, uuid.New())
	if got || err == nil {
		t.Fatalf("Exists() = (%v, %v), want false and error", got, err)
	}
}
