// Package nats pkg/transport/nats/errors.go
package nats

import (
	"fmt"

	"github.com/mfreeman451/goflow2/v2/transport"
)

type TransportError struct {
	Err error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("nats transport: %s", e.Err.Error())
}

func (e *TransportError) Unwrap() []error {
	return []error{transport.ErrTransport, e.Err}
}
