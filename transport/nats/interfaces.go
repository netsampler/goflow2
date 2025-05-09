// Package nats pkg/transport/nats/interfaces.go
package nats

import (
	"context"
)

// JetStreamPublisher defines an interface for publishing messages to a NATS JetStream subject.
type JetStreamPublisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}
