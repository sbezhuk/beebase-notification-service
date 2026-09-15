package firebase

import (
	"context"
	"firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"fmt"
	appnotification "github.com/sbezhuk/beebase-notification-service/internal/application/notification"
	"google.golang.org/api/option"
)

type Sender struct{ client *messaging.Client }

func NewSender(ctx context.Context, projectID string, serviceAccountJSON []byte) (*Sender, error) {
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, option.WithCredentialsJSON(serviceAccountJSON))
	if err != nil {
		return nil, fmt.Errorf("firebase: initialize app: %w", err)
	}
	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase: initialize messaging: %w", err)
	}
	return &Sender{client: client}, nil
}
func (s *Sender) Send(ctx context.Context, m appnotification.PushMessage) error {
	_, err := s.client.Send(ctx, &messaging.Message{Fid: m.Destination, Notification: &messaging.Notification{Title: m.Title, Body: m.Body}, Data: m.Data})
	if err != nil {
		kind := classifyError(err)
		return &appnotification.DeliveryError{Kind: kind, Err: fmt.Errorf("firebase send: %w", err)}
	}
	return nil
}

func classifyError(err error) appnotification.DeliveryErrorKind {
	// FCM can return INVALID_ARGUMENT as the outer status for an unregistered
	// FID. IsUnregistered checks the provider-specific FcmError detail, so it
	// must be evaluated before the generic status helper.
	switch {
	case messaging.IsUnregistered(err):
		return appnotification.DeliveryUnregistered
	case messaging.IsThirdPartyAuthError(err), messaging.IsSenderIDMismatch(err):
		return appnotification.DeliveryAuthentication
	case messaging.IsUnavailable(err):
		return appnotification.DeliveryTemporary
	case messaging.IsInvalidArgument(err):
		// INVALID_ARGUMENT does not by itself establish that the destination is
		// stale; it may describe an unrelated malformed request field.
		return appnotification.DeliveryUnknown
	default:
		return appnotification.DeliveryUnknown
	}
}
