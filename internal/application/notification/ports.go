package notification

import (
	"context"
	"fmt"
)

type PushMessage struct {
	Destination, Title, Body string
	Data                     map[string]string
}

type PushSender interface {
	Send(context.Context, PushMessage) error
}

type DeliveryErrorKind string

const (
	DeliveryInvalidDestination DeliveryErrorKind = "invalid_destination"
	DeliveryUnregistered       DeliveryErrorKind = "unregistered_destination"
	DeliveryAuthentication     DeliveryErrorKind = "firebase_authentication"
	DeliveryTemporary          DeliveryErrorKind = "temporary_firebase_failure"
	DeliveryUnknown            DeliveryErrorKind = "firebase_failure"
)

type DeliveryError struct {
	Kind DeliveryErrorKind
	Err  error
}

func (e *DeliveryError) Error() string { return fmt.Sprintf("%s: %v", e.Kind, e.Err) }
func (e *DeliveryError) Unwrap() error { return e.Err }
