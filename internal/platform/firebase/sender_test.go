package firebase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	firebaseapp "firebase.google.com/go/v4"
	appnotification "github.com/sbezhuk/beebase-notification-service/internal/application/notification"
	"google.golang.org/api/option"
)

func TestSenderClassifiesFCMDetailBeforeOuterStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		detail string
		want   appnotification.DeliveryErrorKind
	}{
		{name: "unregistered detail", detail: "UNREGISTERED", want: appnotification.DeliveryUnregistered},
		{name: "generic invalid argument", detail: "INVALID_ARGUMENT", want: appnotification.DeliveryUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":400,"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"` + tc.detail + `"}]}}`))
			}))
			defer server.Close()

			app, err := firebaseapp.NewApp(context.Background(), &firebaseapp.Config{ProjectID: "test-project"}, option.WithoutAuthentication(), option.WithEndpoint(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			client, err := app.Messaging(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			sender := &Sender{client: client}

			err = sender.Send(context.Background(), appnotification.PushMessage{Destination: "fid-test", Title: "title", Body: "body"})
			if err == nil {
				t.Fatal("Send() succeeded; want classified error")
			}
			var deliveryErr *appnotification.DeliveryError
			if !asDeliveryError(err, &deliveryErr) || deliveryErr.Kind != tc.want {
				t.Fatalf("classification = %v, want %v", err, tc.want)
			}
		})
	}
}

func asDeliveryError(err error, target **appnotification.DeliveryError) bool {
	if deliveryErr, ok := err.(*appnotification.DeliveryError); ok {
		*target = deliveryErr
		return true
	}
	return false
}
