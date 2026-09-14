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
		kind := appnotification.DeliveryUnknown
		switch {
		case messaging.IsInvalidArgument(err):
			kind = appnotification.DeliveryInvalidDestination
		case messaging.IsUnregistered(err):
			kind = appnotification.DeliveryUnregistered
		case messaging.IsThirdPartyAuthError(err), messaging.IsSenderIDMismatch(err):
			kind = appnotification.DeliveryAuthentication
		case messaging.IsUnavailable(err):
			kind = appnotification.DeliveryTemporary
		}
		return &appnotification.DeliveryError{Kind: kind, Err: fmt.Errorf("firebase send: %w", err)}
	}
	return nil
}
